package recordings

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"
	"modernc.org/sqlite"
	"voxhold-backend/internal/storagebudget"
)

const MaxFileBytes int64 = 2 << 30
const ChunkBytes = 1 << 20
const Retention = 24 * time.Hour

// The instance owner may reserve 1 GiB-1 TiB; files above the quota stay local.
const DefaultQuotaBytes int64 = 6 << 30
const MinQuotaBytes int64 = 1 << 30
const MaxQuotaBytes int64 = 1 << 40

var ErrNotFound = errors.New("recording not found")
var ErrConflict = errors.New("recording state changed or another recording is active")
var ErrLimit = errors.New("server recording storage limit reached; the local file is preserved")
var ErrForbidden = errors.New("only the instance owner can manage recording storage")
var ErrInvalidQuota = errors.New("recording storage must be between 1 and 1024 GiB")

type Record struct {
	ID             string  `json:"id"`
	ServerID       int64   `json:"server_id"`
	ChannelID      int64   `json:"channel_id"`
	OwnerUserID    int64   `json:"owner_user_id"`
	OwnerUsername  string  `json:"owner_username"`
	Status         string  `json:"status"`
	MIMEType       string  `json:"mime_type"`
	SizeBytes      int64   `json:"size_bytes"`
	DurationMS     int64   `json:"duration_ms"`
	CreatedAt      int64   `json:"created_at"`
	UpdatedAt      int64   `json:"updated_at"`
	ExpiresAt      int64   `json:"expires_at"`
	PreviewVersion *string `json:"preview_version,omitempty"`
}

func (r Record) extension() string {
	if r.MIMEType == "video/mp4" {
		return ".mp4"
	}
	return ".webm"
}
func (r Record) Filename() string {
	return "voxhold-" + time.Unix(r.CreatedAt, 0).UTC().Format("20060102-150405") + "-" + r.ID[:8] + r.extension()
}

type Config struct {
	Directory              string
	QuotaBytes             int64
	DownloadBytesPerSecond int
}
type Storage struct {
	QuotaBytes        int64 `json:"quota_bytes"`
	UsedBytes         int64 `json:"used_bytes"`
	DefaultQuotaBytes int64 `json:"default_quota_bytes"`
	MinQuotaBytes     int64 `json:"min_quota_bytes"`
	MaxQuotaBytes     int64 `json:"max_quota_bytes"`
	MaxFileBytes      int64 `json:"max_file_bytes"`
	RetentionSeconds  int64 `json:"retention_seconds"`
}
type Service struct {
	db           *sql.DB
	root         *os.Root
	config       Config
	quota        atomic.Int64 // Owner setting, or Config.QuotaBytes when unset.
	mu           sync.Mutex   // Serializes only short metadata/chunk mutations, never RTP or downloads.
	key          [32]byte
	now          func() time.Time
	publish      func(Record)
	present      func(int64, int64, int64) bool
	downloadRate *rate.Limiter
}

func New(db *sql.DB, config Config, present func(int64, int64, int64) bool, publish func(Record)) (*Service, error) {
	if config.Directory == "" {
		config.Directory = filepath.Join("data", "recordings")
	}
	if config.QuotaBytes == 0 {
		config.QuotaBytes = DefaultQuotaBytes
	}
	if config.QuotaBytes < MinQuotaBytes || config.QuotaBytes > MaxQuotaBytes {
		return nil, fmt.Errorf("recording quota must be between 1 GiB and 1 TiB")
	}
	var configured sql.NullInt64
	if err := db.QueryRow(`SELECT recording_quota_bytes FROM instance_settings WHERE id=1`).Scan(&configured); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("load recording quota: %w", err)
	}
	if config.DownloadBytesPerSecond == 0 {
		config.DownloadBytesPerSecond = 2 << 20
	}
	if config.DownloadBytesPerSecond < 32<<10 {
		return nil, fmt.Errorf("recording download rate must be at least 32 KiB/s")
	}
	if err := os.MkdirAll(config.Directory, 0700); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(config.Directory)
	if err != nil {
		return nil, err
	}
	s := &Service{db: db, root: root, config: config, now: time.Now, publish: publish, present: present, downloadRate: rate.NewLimiter(rate.Limit(config.DownloadBytesPerSecond), 256<<10)}
	s.quota.Store(config.QuotaBytes)
	if configured.Valid {
		s.quota.Store(configured.Int64)
	}
	if _, err = rand.Read(s.key[:]); err != nil {
		root.Close()
		return nil, err
	}
	if _, err = db.Exec(`UPDATE channel_recordings SET status='failed',updated_at=max(updated_at+1,?) WHERE status='recording'`, time.Now().UnixMilli()); err != nil {
		root.Close()
		return nil, err
	}
	return s, nil
}
func (s *Service) Close() error { return s.root.Close() }

// ApplyBudget updates the reservation ceiling after the owner's shared allocation.
func (s *Service) ApplyBudget(quota int64) { s.quota.Store(quota) }
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Cleanup(ctx); err != nil {
				log.Printf("recording cleanup: %v", err)
			}
		}
	}
}

const columns = `r.id,r.server_id,r.channel_id,r.owner_user_id,u.username,r.status,r.mime_type,r.size_bytes,r.duration_ms,r.created_at,r.updated_at,r.expires_at,(SELECT etag FROM recording_previews WHERE recording_id=r.id)`

type scanner interface{ Scan(...any) error }

func scan(row scanner) (Record, error) {
	var r Record
	var preview sql.NullString
	err := row.Scan(&r.ID, &r.ServerID, &r.ChannelID, &r.OwnerUserID, &r.OwnerUsername, &r.Status, &r.MIMEType, &r.SizeBytes, &r.DurationMS, &r.CreatedAt, &r.UpdatedAt, &r.ExpiresAt, &preview)
	if preview.Valid {
		r.PreviewVersion = &preview.String
	}
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}
func validID(id string) bool {
	if len(id) != 32 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}
func (s *Service) Get(ctx context.Context, id string, userID int64) (Record, error) {
	if !validID(id) {
		return Record{}, ErrNotFound
	}
	return scan(s.db.QueryRowContext(ctx, `SELECT `+columns+` FROM channel_recordings r JOIN users u ON u.id=r.owner_user_id WHERE r.id=? AND r.expires_at>? AND EXISTS(SELECT 1 FROM server_members m JOIN users viewer ON viewer.id=m.user_id WHERE m.server_id=r.server_id AND m.user_id=? AND EXISTS(SELECT 1 FROM channel_accessible_members a WHERE a.channel_id=r.channel_id AND a.user_id=m.user_id) AND viewer.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM user_bans WHERE user_id=viewer.id))`, id, s.now().Unix(), userID))
}
func (s *Service) List(ctx context.Context, serverID, userID int64) ([]Record, error) {
	var allowed bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM server_members WHERE server_id=? AND user_id=?)`, serverID, userID).Scan(&allowed); err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrNotFound
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+columns+` FROM channel_recordings r JOIN users u ON u.id=r.owner_user_id WHERE r.server_id=? AND r.expires_at>? AND EXISTS(SELECT 1 FROM channel_accessible_members a WHERE a.channel_id=r.channel_id AND a.user_id=?) ORDER BY CASE WHEN r.status='recording' THEN 0 ELSE 1 END,r.created_at DESC,r.id DESC LIMIT 100`, serverID, s.now().Unix(), userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Record, 0)
	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}
func (s *Service) emit(r Record) {
	if s.publish != nil {
		s.publish(r)
	}
}

func (s *Service) Start(ctx context.Context, serverID, channelID, userID int64, mime string) (Record, error) {
	if mime != "video/webm" && mime != "video/mp4" {
		return Record{}, ErrConflict
	}
	if s.present == nil || !s.present(userID, serverID, channelID) {
		return Record{}, ErrNotFound
	}
	s.mu.Lock()
	var idBytes [16]byte
	if _, err := rand.Read(idBytes[:]); err != nil {
		s.mu.Unlock()
		return Record{}, err
	}
	id := hex.EncodeToString(idBytes[:])
	now := s.now()
	result, err := s.db.ExecContext(ctx, `INSERT INTO channel_recordings(id,server_id,channel_id,owner_user_id,status,mime_type,updated_at,expires_at) SELECT ?,?,?,?,'recording',?,?,? WHERE EXISTS(SELECT 1 FROM channels c JOIN server_members m ON m.server_id=c.server_id WHERE c.id=? AND c.server_id=? AND c.kind='voice' AND m.user_id=? AND EXISTS(SELECT 1 FROM channel_accessible_members a WHERE a.channel_id=c.id AND a.user_id=m.user_id)) AND (SELECT count(*) FROM channel_recordings WHERE status='recording')<20`, id, serverID, channelID, userID, mime, now.UnixMilli(), now.Add(Retention+2*time.Minute).Unix(), channelID, serverID, userID)
	s.mu.Unlock()
	if err != nil {
		var constraint *sqlite.Error
		if errors.As(err, &constraint) && constraint.Code()&0xff == 19 {
			return Record{}, ErrConflict
		}
		return Record{}, err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return Record{}, ErrNotFound
	}
	r, err := s.Get(ctx, id, userID)
	if err == nil {
		s.emit(r)
	}
	return r, err
}
func (s *Service) Heartbeat(ctx context.Context, id string, userID int64) error {
	r, err := s.Get(ctx, id, userID)
	if err != nil {
		return err
	}
	if r.OwnerUserID != userID || r.Status != "recording" || s.present == nil || !s.present(userID, r.ServerID, r.ChannelID) {
		return ErrConflict
	}
	_, err = s.db.ExecContext(ctx, `UPDATE channel_recordings SET heartbeat_at=? WHERE id=? AND status='recording'`, s.now().Unix(), id)
	return err
}

func (s *Service) Stop(ctx context.Context, id string, userID, size, duration int64, localOnly bool) (Record, error) {
	storagebudget.Mu.Lock()
	defer storagebudget.Mu.Unlock()
	if size < 0 || duration < 0 || duration > 24*60*60*1000 {
		return Record{}, ErrConflict
	}
	s.mu.Lock()
	r, err := s.Get(ctx, id, userID)
	if err != nil {
		s.mu.Unlock()
		return r, err
	}
	if r.OwnerUserID != userID || r.Status == "ready" {
		s.mu.Unlock()
		return r, ErrConflict
	}
	if r.Status == "uploading" && !localOnly && size <= MaxFileBytes && size != r.SizeBytes {
		s.mu.Unlock()
		return r, ErrConflict
	}
	state := "uploading"
	if localOnly || size > MaxFileBytes || size == 0 {
		state = "local_only"
		size = 0
	}
	if state == "uploading" {
		used, err := s.used(ctx, id)
		if err != nil {
			s.mu.Unlock()
			return r, err
		}
		if used+size > s.quota.Load() {
			s.mu.Unlock()
			return r, ErrLimit
		}
		var configured bool
		if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM storage_budgets)`).Scan(&configured); err != nil {
			s.mu.Unlock()
			return r, err
		}
		if configured {
			state, err := storagebudget.Get(ctx, s.db, s.config.Directory)
			if err != nil {
				s.mu.Unlock()
				return r, err
			}
			managed := int64(0)
			for _, value := range state.Categories {
				managed += value.Used
			}
			if r.Status == "uploading" {
				managed -= r.SizeBytes
			}
			if size > state.Capacity-managed {
				s.mu.Unlock()
				return r, ErrLimit
			}
		}
	}
	_, err = s.db.ExecContext(ctx, `UPDATE channel_recordings SET status=?,size_bytes=?,duration_ms=?,updated_at=max(updated_at+1,?),expires_at=? WHERE id=?`, state, size, duration, s.now().UnixMilli(), s.now().Add(Retention).Unix(), id)
	if state == "local_only" {
		_ = s.root.Remove(id + ".part")
	}
	s.mu.Unlock()
	if err != nil {
		return r, err
	}
	r, err = s.Get(ctx, id, userID)
	if err == nil {
		s.emit(r)
	}
	return r, err
}

// used counts reserved uploads and ready files, except the recording being resized.
func (s *Service) used(ctx context.Context, exceptID string) (int64, error) {
	var used int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(sum(r.size_bytes+COALESCE(length(p.content),0)),0) FROM channel_recordings r LEFT JOIN recording_previews p ON p.recording_id=r.id WHERE r.status IN ('uploading','ready') AND r.expires_at>? AND r.id<>?`, s.now().Unix(), exceptID).Scan(&used)
	return used, err
}
func (s *Service) requireOwner(ctx context.Context, userID int64) error {
	var owner bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM server_members WHERE user_id=? AND role='owner')`, userID).Scan(&owner); err != nil {
		return err
	}
	if !owner {
		return ErrForbidden
	}
	return nil
}
func (s *Service) Storage(ctx context.Context, userID int64) (Storage, error) {
	if err := s.requireOwner(ctx, userID); err != nil {
		return Storage{}, err
	}
	used, err := s.used(ctx, "")
	if err != nil {
		return Storage{}, err
	}
	return Storage{QuotaBytes: s.quota.Load(), UsedBytes: used, DefaultQuotaBytes: s.config.QuotaBytes, MinQuotaBytes: MinQuotaBytes, MaxQuotaBytes: MaxQuotaBytes, MaxFileBytes: MaxFileBytes, RetentionSeconds: int64(Retention / time.Second)}, nil
}

// SetQuota applies to new uploads only: files above a lowered quota expire normally.
func (s *Service) SetQuota(ctx context.Context, userID, quota int64) (Storage, error) {
	if err := s.requireOwner(ctx, userID); err != nil {
		return Storage{}, err
	}
	if quota < MinQuotaBytes || quota > MaxQuotaBytes {
		return Storage{}, ErrInvalidQuota
	}
	storagebudget.Mu.Lock()
	defer storagebudget.Mu.Unlock()
	var configured int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM storage_budgets`).Scan(&configured); err != nil {
		return Storage{}, err
	}
	if configured > 0 {
		state, err := storagebudget.Get(ctx, s.db, s.config.Directory)
		if err != nil {
			return Storage{}, err
		}
		other := state.Categories["images"].Quota + state.Categories["files"].Quota
		if quota > state.Capacity-other {
			return Storage{}, storagebudget.ErrBudget
		}
	}
	s.mu.Lock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.mu.Unlock()
		return Storage{}, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `UPDATE instance_settings SET recording_quota_bytes=? WHERE id=1`, quota)
	if err == nil && configured > 0 {
		_, err = tx.ExecContext(ctx, `UPDATE storage_budgets SET quota_bytes=? WHERE category='recordings'`, quota)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err == nil {
		s.quota.Store(quota)
	}
	s.mu.Unlock()
	if err != nil {
		return Storage{}, err
	}
	return s.Storage(ctx, userID)
}
func (s *Service) Offset(ctx context.Context, id string, userID int64) (int64, error) {
	r, err := s.Get(ctx, id, userID)
	if err != nil {
		return 0, err
	}
	if r.OwnerUserID != userID || r.Status != "uploading" {
		return 0, ErrConflict
	}
	f, err := s.root.Open(id + ".part")
	if errors.Is(err, os.ErrNotExist) {
		// A crash after rename but before committing metadata is recoverable.
		if info, finalErr := s.root.Stat(id + r.extension()); finalErr == nil && info.Size() == r.SizeBytes {
			return info.Size(), nil
		}
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}
func (s *Service) Append(ctx context.Context, id string, userID, offset int64, data []byte) (int64, error) {
	if len(data) == 0 || len(data) > ChunkBytes || offset < 0 {
		return 0, ErrConflict
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.Get(ctx, id, userID)
	if err != nil {
		return 0, err
	}
	if r.OwnerUserID != userID || r.Status != "uploading" {
		return 0, ErrConflict
	}
	f, err := s.root.OpenFile(id+".part", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return 0, err
	}
	if info.Size() != offset {
		return info.Size(), ErrConflict
	}
	if offset+int64(len(data)) > r.SizeBytes || offset+int64(len(data)) > MaxFileBytes {
		return offset, ErrLimit
	}
	if _, err = f.WriteAt(data, offset); err != nil {
		return offset, err
	}
	return offset + int64(len(data)), nil
}
func (s *Service) Complete(ctx context.Context, id string, userID int64) (Record, error) {
	s.mu.Lock()
	r, err := s.Get(ctx, id, userID)
	if err != nil {
		s.mu.Unlock()
		return r, err
	}
	if r.OwnerUserID != userID {
		s.mu.Unlock()
		return r, ErrNotFound
	}
	if r.Status == "ready" {
		s.mu.Unlock()
		return r, nil
	}
	if r.Status != "uploading" {
		s.mu.Unlock()
		return r, ErrConflict
	}
	f, err := s.root.OpenFile(id+".part", os.O_RDWR, 0600)
	alreadyRenamed := false
	if errors.Is(err, os.ErrNotExist) {
		f, err = s.root.OpenFile(id+r.extension(), os.O_RDWR, 0600)
		alreadyRenamed = err == nil
	}
	if err != nil {
		s.mu.Unlock()
		return r, err
	}
	info, err := f.Stat()
	if err == nil && info.Size() != r.SizeBytes {
		err = ErrConflict
	}
	var header [12]byte
	if err == nil {
		_, err = io.ReadFull(f, header[:])
	}
	if err == nil {
		webm := header[0] == 0x1a && header[1] == 0x45 && header[2] == 0xdf && header[3] == 0xa3
		mp4 := string(header[4:8]) == "ftyp"
		if (r.MIMEType == "video/webm" && !webm) || (r.MIMEType == "video/mp4" && !mp4) {
			err = ErrConflict
		}
	}
	if err == nil {
		err = f.Sync()
	}
	_ = f.Close()
	if err == nil && !alreadyRenamed {
		err = s.root.Rename(id+".part", id+r.extension())
	}
	if err == nil {
		_, err = s.db.ExecContext(ctx, `UPDATE channel_recordings SET status='ready',updated_at=max(updated_at+1,?),expires_at=? WHERE id=?`, s.now().UnixMilli(), s.now().Add(Retention).Unix(), id)
	}
	s.mu.Unlock()
	if err != nil {
		return r, err
	}
	r, err = s.Get(ctx, id, userID)
	if err == nil {
		s.emit(r)
	}
	return r, err
}
func (s *Service) Ticket(ctx context.Context, id string, userID int64) (string, error) {
	r, err := s.Get(ctx, id, userID)
	if err != nil {
		return "", err
	}
	if r.Status != "ready" {
		return "", ErrConflict
	}
	payload := fmt.Sprintf("%s:%d:%d", id, userID, s.now().Add(5*time.Minute).Unix())
	mac := hmac.New(sha256.New, s.key[:])
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}
func (s *Service) OpenDownload(ctx context.Context, ticket string) (Record, *os.File, error) {
	parts := strings.Split(ticket, ".")
	if len(parts) != 2 {
		return Record{}, nil, ErrNotFound
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Record{}, nil, ErrNotFound
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Record{}, nil, ErrNotFound
	}
	mac := hmac.New(sha256.New, s.key[:])
	_, _ = mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return Record{}, nil, ErrNotFound
	}
	fields := strings.Split(string(payload), ":")
	if len(fields) != 3 {
		return Record{}, nil, ErrNotFound
	}
	userID, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return Record{}, nil, ErrNotFound
	}
	expires, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil || expires <= s.now().Unix() {
		return Record{}, nil, ErrNotFound
	}
	r, err := s.Get(ctx, fields[0], userID)
	if err != nil {
		return r, nil, err
	}
	if r.Status != "ready" {
		return r, nil, ErrNotFound
	}
	f, err := s.root.Open(r.ID + r.extension())
	return r, f, err
}
func (s *Service) Cleanup(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.QueryContext(ctx, `SELECT `+columns+` FROM channel_recordings r JOIN users u ON u.id=r.owner_user_id WHERE r.expires_at<=? OR (r.status='recording' AND r.heartbeat_at<?)`, s.now().Unix(), s.now().Add(-2*time.Minute).Unix())
	if err != nil {
		return err
	}
	var expired []Record
	for rows.Next() {
		r, e := scan(rows)
		if e != nil {
			rows.Close()
			return e
		}
		expired = append(expired, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, r := range expired {
		if r.ExpiresAt > s.now().Unix() {
			_, err = s.db.ExecContext(ctx, `UPDATE channel_recordings SET status='failed',updated_at=max(updated_at+1,?) WHERE id=?`, s.now().UnixMilli(), r.ID)
			if err == nil {
				r.Status = "failed"
				r.UpdatedAt = max(r.UpdatedAt+1, s.now().UnixMilli())
				s.emit(r)
			}
			if err != nil {
				return err
			}
			continue
		}
		for _, suffix := range []string{".part", ".webm", ".mp4"} {
			e := s.root.Remove(r.ID + suffix)
			if e != nil && !errors.Is(e, os.ErrNotExist) {
				return e
			}
		}
		if _, err = s.db.ExecContext(ctx, "DELETE FROM channel_recordings WHERE id=?", r.ID); err != nil {
			return err
		}
	}
	dir, err := s.root.Open(".")
	if err != nil {
		return err
	}
	entries, err := dir.ReadDir(-1)
	dir.Close()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		id := strings.TrimSuffix(name, filepath.Ext(name))
		if !validID(id) || (filepath.Ext(name) != ".part" && filepath.Ext(name) != ".webm" && filepath.Ext(name) != ".mp4") {
			continue
		}
		var exists bool
		if err = s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM channel_recordings WHERE id=?)", id).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			if err = s.root.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	return nil
}

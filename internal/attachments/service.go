// Package attachments stores authenticated message uploads in the instance data volume.
package attachments

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
	"image"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
	"voxhold-backend/internal/community"
	"voxhold-backend/internal/httpapi"
	"voxhold-backend/internal/imagelimit"
	"voxhold-backend/internal/message"
	"voxhold-backend/internal/permissions"
	"voxhold-backend/internal/recordings"
	"voxhold-backend/internal/storagebudget"
)

const MaxFileBytes int64 = 64 << 20

type Plan struct {
	Category string    `json:"category"`
	Before   int64     `json:"before"`
	Bytes    int64     `json:"bytes"`
	Count    int       `json:"count"`
	Token    string    `json:"token"`
	IDs      []string  `json:"-"`
	Expires  time.Time `json:"-"`
}
type Service struct {
	db             *sql.DB
	root           *os.Root
	directory      string
	mu             sync.Mutex
	cleanupMu      sync.Mutex
	cleanupCursor  string
	uploads        chan struct{}
	plans          map[string]Plan
	recordings     *recordings.Service
	changed        func(int64, int64, []string)
	thumbMu        sync.Mutex
	thumbnails     map[string][]byte
	thumbnailOrder []string
	thumbnailSlots chan struct{}
}

func New(db *sql.DB, recordingService *recordings.Service, changed func(int64, int64, []string)) (*Service, error) {
	dir := storagebudget.Directory()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	s := &Service{db: db, root: root, directory: dir, uploads: make(chan struct{}, 2), thumbnails: map[string][]byte{}, thumbnailSlots: make(chan struct{}, 2), plans: map[string]Plan{}, recordings: recordingService, changed: changed}
	state, err := storagebudget.Get(context.Background(), db, dir)
	if err != nil {
		root.Close()
		return nil, err
	}
	// The existing recording ceiling may come from the operator's environment.
	// Preserve it when initializing joint allocations for the first time.
	var previous sql.NullInt64
	if err = db.QueryRow(`SELECT recording_quota_bytes FROM instance_settings WHERE id=1`).Scan(&previous); err != nil {
		root.Close()
		return nil, err
	}
	if !previous.Valid {
		recording := state.Categories["recordings"]
		recording.Quota = min(recordingService.QuotaBytes(), state.Capacity)
		state.Categories["recordings"] = recording
		for _, category := range []string{"images", "files"} {
			value := state.Categories[category]
			value.Quota = max(0, (state.Capacity-recording.Quota)/2)
			state.Categories[category] = value
		}
	}
	tx, err := db.Begin()
	if err != nil {
		root.Close()
		return nil, err
	}
	defer tx.Rollback()
	for category, value := range state.Categories {
		if _, err = tx.Exec(`INSERT INTO storage_budgets(category,quota_bytes) VALUES(?,?) ON CONFLICT DO NOTHING`, category, value.Quota); err != nil {
			root.Close()
			return nil, err
		}
	}
	if _, err = tx.Exec(`UPDATE instance_settings SET recording_quota_bytes=(SELECT quota_bytes FROM storage_budgets WHERE category='recordings') WHERE id=1`); err != nil {
		root.Close()
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		root.Close()
		return nil, err
	}
	recordingService.ApplyBudget(state.Categories["recordings"].Quota)
	return s, nil
}
func (s *Service) Close() error { return s.root.Close() }
func randomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func validID(id string) bool {
	v, err := hex.DecodeString(id)
	// Canonical IDs also keep SQLite's case-sensitive keys consistent with
	// case-insensitive filesystems: a case variant must never replace a file.
	return err == nil && len(v) == 16 && len(id) == 32 && id == strings.ToLower(id)
}
func (s *Service) RegisterRoutes(mux *http.ServeMux, auth func(http.Handler) http.Handler) {
	mux.Handle("PUT /api/v1/channels/{channelID}/attachments/{attachmentID}", auth(http.HandlerFunc(s.upload)))
	mux.Handle("PUT /api/v1/conversations/{conversationID}/attachments/{attachmentID}", auth(http.HandlerFunc(s.upload)))
	mux.Handle("GET /api/v1/attachments/{attachmentID}/content", auth(http.HandlerFunc(s.content)))
	mux.Handle("GET /api/v1/attachments/{attachmentID}/thumbnail", auth(http.HandlerFunc(s.thumbnail)))
	mux.Handle("GET /api/v1/instance/storage", auth(http.HandlerFunc(s.storage)))
	mux.Handle("PUT /api/v1/instance/storage", auth(http.HandlerFunc(s.storage)))
	mux.Handle("POST /api/v1/instance/storage/preview", auth(http.HandlerFunc(s.preview)))
	mux.Handle("POST /api/v1/instance/storage/cleanup", auth(http.HandlerFunc(s.cleanup)))
}
func fail(w http.ResponseWriter, code int, description string) {
	httpapi.WriteError(w, code, description)
}
func (s *Service) owner(ctx context.Context, id int64) bool {
	var yes bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM server_members WHERE user_id=? AND role='owner')`, id).Scan(&yes)
	return err == nil && yes
}
func (s *Service) channel(ctx context.Context, cid, uid int64) bool {
	var yes bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM channel_accessible_members a JOIN channels c ON c.id=a.channel_id JOIN member_permissions p ON p.server_id=c.server_id AND p.user_id=a.user_id WHERE a.channel_id=? AND a.user_id=? AND (p.permissions & ?)<>0 AND (c.is_news=0 OR c.comments_enabled=1 OR EXISTS(SELECT 1 FROM server_members WHERE server_id=c.server_id AND user_id=? AND role IN ('owner','admin')) OR EXISTS(SELECT 1 FROM channel_publishers WHERE channel_id=c.id AND user_id=?)))`, cid, uid, permissions.Publish, uid, uid).Scan(&yes)
	return err == nil && yes
}
func (s *Service) upload(w http.ResponseWriter, r *http.Request) {
	uid, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(2 * time.Minute))
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(2 * time.Minute))
	var cid, conversationID int64
	if r.PathValue("conversationID") != "" {
		conversationID, ok = httpapi.PositiveInt64PathValue(w, r, "conversationID", "conversation")
	} else {
		cid, ok = httpapi.PositiveInt64PathValue(w, r, "channelID", "channel")
	}
	if !ok {
		return
	}
	canUpload := func() bool {
		if conversationID > 0 {
			return community.CanSend(r.Context(), s.db, conversationID, uid) == nil
		}
		return s.channel(r.Context(), cid, uid)
	}
	id := r.PathValue("attachmentID")
	if !validID(id) {
		fail(w, 400, "invalid attachment ID")
		return
	}
	if !canUpload() {
		fail(w, 403, "upload unavailable")
		return
	}
	select {
	case s.uploads <- struct{}{}:
		defer func() { <-s.uploads }()
	default:
		fail(w, 429, "uploads busy; retry shortly")
		return
	}
	var existing message.Attachment
	var owner, c, conv int64
	var deleted bool
	err := s.db.QueryRowContext(r.Context(), `SELECT id,filename,mime_type,size_bytes,owner_user_id,COALESCE(channel_id,0),COALESCE(conversation_id,0),deleted_at IS NOT NULL FROM attachments WHERE id=?`, id).Scan(&existing.ID, &existing.Filename, &existing.MIMEType, &existing.SizeBytes, &owner, &c, &conv, &deleted)
	if err == nil {
		if owner != uid || c != cid || conv != conversationID || deleted {
			fail(w, 409, "attachment ID already used")
			return
		}
		httpapi.WriteJSON(w, 200, existing)
		return
	}
	if err != sql.ErrNoRows {
		fail(w, 500, "upload metadata unavailable")
		return
	}
	name := strings.TrimSpace(filepath.Base(strings.ReplaceAll(r.URL.Query().Get("filename"), "\\", "/")))
	if name == "" || name == "." || utf8.RuneCountInString(name) > 200 || strings.ContainsAny(name, "\r\n\x00") {
		fail(w, 400, "invalid filename")
		return
	}
	if r.ContentLength > MaxFileBytes {
		fail(w, 413, "attachment exceeds 64 MiB")
		return
	}
	// Write a random temporary file, then validate and commit under the quota lock.
	temp := randomID() + ".part"
	file, err := s.root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		fail(w, 500, "upload unavailable")
		return
	}
	defer s.root.Remove(temp)
	size, err := io.Copy(file, http.MaxBytesReader(w, r.Body, MaxFileBytes))
	closeErr := file.Close()
	if err != nil {
		fail(w, 413, "upload interrupted or exceeds 64 MiB")
		return
	}
	if closeErr != nil || size == 0 {
		fail(w, 400, "empty or incomplete file")
		return
	}
	file, err = s.root.Open(temp)
	if err != nil {
		fail(w, 500, "upload unavailable")
		return
	}
	head := make([]byte, 512)
	n, _ := file.Read(head)
	file.Close()
	mimeType := http.DetectContentType(head[:n])
	category := "files"
	if strings.HasPrefix(mimeType, "image/") && mimeType != "image/svg+xml" {
		file, err = s.root.Open(temp)
		if err != nil {
			fail(w, 500, "upload unavailable")
			return
		}
		compressed, typ, err := compressImage(file)
		closeErr := file.Close()
		if closeErr != nil {
			fail(w, 500, "upload unavailable")
			return
		}
		if err != nil {
			fail(w, 400, "invalid image or image exceeds 30 megapixels")
			return
		}
		mimeType = typ
		size = int64(len(compressed))
		if err = s.root.WriteFile(temp, compressed, 0600); err != nil {
			fail(w, 500, "upload unavailable")
			return
		}
		category = "images"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	storagebudget.Mu.Lock()
	defer storagebudget.Mu.Unlock()
	state, err := storagebudget.Get(r.Context(), s.db, s.directory)
	if err != nil {
		fail(w, 500, "storage unavailable")
		return
	}
	budget := state.Categories[category]
	managed := int64(0)
	for _, value := range state.Categories {
		managed += value.Used
	}
	if size > budget.Quota-budget.Used || size > state.Capacity-managed {
		fail(w, 507, "category storage quota reached")
		return
	}
	// Recheck authorization after receiving the body: role changes can happen mid-upload.
	if !canUpload() {
		fail(w, 403, "upload access revoked")
		return
	}
	var exists bool
	if err = s.db.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM attachments WHERE id=?)`, id).Scan(&exists); err != nil {
		fail(w, 500, "upload unavailable")
		return
	}
	if exists {
		fail(w, 409, "upload already completed; retry")
		return
	}
	if err = s.root.Rename(temp, id); err != nil {
		fail(w, 500, "upload unavailable")
		return
	}
	_, err = s.db.ExecContext(r.Context(), `INSERT INTO attachments(id,channel_id,conversation_id,owner_user_id,filename,mime_type,category,size_bytes) VALUES(?,NULLIF(?,0),NULLIF(?,0),?,?,?,?,?)`, id, cid, conversationID, uid, name, mimeType, category, size)
	if err != nil {
		s.root.Remove(id)
		fail(w, 500, "upload unavailable")
		return
	}
	httpapi.WriteJSON(w, 200, message.Attachment{ID: id, Filename: name, MIMEType: mimeType, SizeBytes: size})
}
func compressImage(file io.ReadSeeker) ([]byte, string, error) {
	config, _, err := image.DecodeConfig(file)
	if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 30_000_000 || imagelimit.DecodedBytes(config) > imagelimit.MaxDecodedBytes {
		return nil, "", errors.New("invalid image")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, "", err
	}
	img, _, err := image.Decode(file)
	if err != nil {
		return nil, "", err
	}
	w, h := config.Width, config.Height
	if w > 2560 || h > 2560 {
		ratio := min(2560/float64(w), 2560/float64(h))
		w = max(1, int(float64(w)*ratio))
		h = max(1, int(float64(h)*ratio))
		resized := image.NewNRGBA(image.Rect(0, 0, w, h))
		draw.CatmullRom.Scale(resized, resized.Bounds(), img, img.Bounds(), draw.Over, nil)
		img = resized
	}
	var out bytes.Buffer
	opaque := true
	if v, ok := img.(interface{ Opaque() bool }); ok {
		opaque = v.Opaque()
	}
	if !opaque {
		err = png.Encode(&out, img)
		return out.Bytes(), "image/png", err
	}
	err = jpeg.Encode(&out, img, &jpeg.Options{Quality: 82})
	return out.Bytes(), "image/jpeg", err
}
func (s *Service) content(w http.ResponseWriter, r *http.Request) {
	uid, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}
	id := r.PathValue("attachmentID")
	if !validID(id) {
		fail(w, 404, "attachment not found")
		return
	}
	name, typ, deleted, err := s.contentMetadata(r.Context(), uid, id)
	if err != nil {
		fail(w, 404, "attachment not found")
		return
	}
	if deleted {
		fail(w, 410, "attachment removed by server")
		return
	}
	f, err := s.root.Open(id)
	if err != nil {
		fail(w, 410, "attachment removed by server")
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		fail(w, 500, "attachment unavailable")
		return
	}
	disposition := "attachment"
	if inlineContentType(typ) {
		disposition = "inline"
	}
	w.Header().Set("Content-Type", typ)
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": name}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, name, stat.ModTime(), f)
}

// Only passive media may open in the application's origin. Keep legacy SVG
// and other document types as downloads even when their stored MIME is image/*.
func inlineContentType(value string) bool {
	typ := strings.ToLower(strings.TrimSpace(strings.SplitN(value, ";", 2)[0]))
	switch typ {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "image/avif",
		"video/mp4", "video/webm", "video/ogg",
		"audio/mpeg", "audio/ogg", "audio/wav", "audio/webm", "audio/mp4":
		return true
	default:
		return false
	}
}
func (s *Service) storage(w http.ResponseWriter, r *http.Request) {
	uid, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}
	if !s.owner(r.Context(), uid) {
		fail(w, 403, "owner required")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	storagebudget.Mu.Lock()
	defer storagebudget.Mu.Unlock()
	state, err := storagebudget.Get(r.Context(), s.db, s.directory)
	if err != nil {
		fail(w, 500, "storage unavailable")
		return
	}
	if r.Method == "PUT" {
		var input struct {
			Images     int64 `json:"images"`
			Files      int64 `json:"files"`
			Recordings int64 `json:"recordings"`
		}
		if !httpapi.DecodeJSON(w, r, &input) {
			return
		}
		if input.Images < 0 || input.Files < 0 || input.Recordings < 0 || input.Images > state.Capacity || input.Files > state.Capacity-input.Images || input.Recordings > state.Capacity-input.Images-input.Files {
			fail(w, 400, "allocations exceed disk budget")
			return
		}
		tx, err := s.db.BeginTx(r.Context(), nil)
		if err != nil {
			fail(w, 500, "storage unavailable")
			return
		}
		defer tx.Rollback()
		for cat, v := range map[string]int64{"images": input.Images, "files": input.Files, "recordings": input.Recordings} {
			if _, err = tx.ExecContext(r.Context(), `INSERT INTO storage_budgets(category,quota_bytes) VALUES(?,?) ON CONFLICT(category) DO UPDATE SET quota_bytes=excluded.quota_bytes`, cat, v); err != nil {
				fail(w, 500, "storage unavailable")
				return
			}
		}
		if _, err = tx.ExecContext(r.Context(), `UPDATE instance_settings SET recording_quota_bytes=? WHERE id=1`, input.Recordings); err != nil {
			fail(w, 500, "storage unavailable")
			return
		}
		if err = tx.Commit(); err != nil {
			fail(w, 500, "storage unavailable")
			return
		}
		s.recordings.ApplyBudget(input.Recordings)
		state, err = storagebudget.Get(r.Context(), s.db, s.directory)
		if err != nil {
			fail(w, 500, "storage unavailable")
			return
		}
	}
	httpapi.WriteJSON(w, 200, state)
}
func (s *Service) preview(w http.ResponseWriter, r *http.Request) {
	uid, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}
	if !s.owner(r.Context(), uid) {
		fail(w, 403, "owner required")
		return
	}
	var input struct {
		Category string `json:"category"`
		Before   int64  `json:"before"`
	}
	if !httpapi.DecodeJSON(w, r, &input) {
		return
	}
	if (input.Category != "images" && input.Category != "files" && input.Category != "recordings") || input.Before <= 0 || input.Before > time.Now().Unix() {
		fail(w, 400, "invalid cleanup period")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	query := `SELECT id,size_bytes FROM attachments WHERE category=? AND deleted_at IS NULL AND created_at<?`
	args := []any{input.Category, input.Before}
	if input.Category == "recordings" {
		query = `SELECT r.id,r.size_bytes+COALESCE(length(p.content),0) FROM channel_recordings r LEFT JOIN recording_previews p ON p.recording_id=r.id WHERE r.status='ready' AND r.created_at<?`
		args = []any{input.Before}
	}
	rows, err := s.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		fail(w, 500, "cleanup unavailable")
		return
	}
	p := Plan{Category: input.Category, Before: input.Before, Token: randomID(), IDs: []string{}, Expires: time.Now().Add(5 * time.Minute)}
	for rows.Next() {
		var id string
		var size int64
		if err = rows.Scan(&id, &size); err != nil {
			break
		}
		p.IDs = append(p.IDs, id)
		p.Bytes += size
		p.Count++
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		fail(w, 500, "cleanup unavailable")
		return
	}
	for key, old := range s.plans {
		if time.Now().After(old.Expires) {
			delete(s.plans, key)
		}
	}
	if len(s.plans) >= 32 {
		fail(w, 429, "too many cleanup previews")
		return
	}
	s.plans[p.Token] = p
	httpapi.WriteJSON(w, 200, p)
}
func (s *Service) cleanup(w http.ResponseWriter, r *http.Request) {
	uid, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}
	if !s.owner(r.Context(), uid) {
		fail(w, 403, "owner required")
		return
	}
	var input struct {
		Token string `json:"token"`
	}
	if !httpapi.DecodeJSON(w, r, &input) {
		return
	}
	s.mu.Lock()
	p, exists := s.plans[input.Token]
	if !exists || time.Now().After(p.Expires) {
		s.mu.Unlock()
		fail(w, 409, "cleanup preview expired; preview again")
		return
	}
	delete(s.plans, input.Token)
	s.mu.Unlock()
	var reclaimed int64
	count := 0
	for _, id := range p.IDs {
		if p.Category == "recordings" {
			v, err := s.recordings.Get(r.Context(), id, uid)
			if err != nil || v.Status != "ready" {
				continue
			}
			var previewSize int64
			if err = s.db.QueryRowContext(r.Context(), `SELECT COALESCE((SELECT length(content) FROM recording_previews WHERE recording_id=?),0)`, id).Scan(&previewSize); err != nil {
				fail(w, 500, "recording cleanup failed")
				return
			}
			if _, err = s.recordings.DeleteForOwner(r.Context(), id, uid); err != nil {
				fail(w, 500, "recording cleanup failed")
				return
			}
			reclaimed += v.SizeBytes + previewSize
			count++
			continue
		}
		var size, cid, conversationID int64
		err := s.db.QueryRowContext(r.Context(), `SELECT size_bytes,COALESCE(channel_id,0),COALESCE(conversation_id,0) FROM attachments WHERE id=? AND deleted_at IS NULL`, id).Scan(&size, &cid, &conversationID)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			fail(w, 500, "cleanup unavailable")
			return
		}
		if _, err = s.db.ExecContext(r.Context(), `UPDATE attachments SET deleted_at=unixepoch() WHERE id=? AND deleted_at IS NULL`, id); err != nil {
			fail(w, 500, "cleanup metadata failed")
			return
		}
		if err = s.root.Remove(id); err != nil && !errors.Is(err, os.ErrNotExist) {
			fail(w, 500, "file cleanup failed")
			return
		}

		reclaimed += size
		count++
		if s.changed != nil {
			s.changed(cid, conversationID, []string{id})
		}
	}
	httpapi.WriteJSON(w, 200, map[string]any{"bytes": reclaimed, "count": count})
}

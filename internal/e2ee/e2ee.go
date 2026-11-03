// Package e2ee stores the public device keys used for end-to-end encrypted
// voice (FEAT-13) and the per-channel encryption policy. The server never sees
// private keys or media keys: clients wrap their sender keys for each other and
// the realtime layer only relays the opaque payloads.
package e2ee

import (
	"context"
	"crypto/ecdh"
	"database/sql"
	"errors"
	"fmt"
	"regexp"

	"voxhold-backend/internal/storage"
)

const (
	// MaxActiveDevices bounds the keys a client has to wrap a sender key for;
	// registering one more revokes the device used least recently.
	MaxActiveDevices = 10
	// MaxRelayPayload is the largest base64 wrapped-key payload the server relays.
	MaxRelayPayload = 1024
)

var (
	ErrInvalidDevice    = errors.New("device id must be 32 lowercase hex characters")
	ErrInvalidPublicKey = errors.New("public key must be an uncompressed P-256 point")
	ErrKeyConflict      = errors.New("device is already registered with another key")
	ErrNotFound         = errors.New("not found")
	ErrForbidden        = errors.New("only owners and administrators can change encryption")
)

var deviceIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// ValidDeviceID reports whether id is the hex form of 16 random bytes.
func ValidDeviceID(id string) bool { return deviceIDPattern.MatchString(id) }

// ValidatePublicKey accepts only a 65-byte uncompressed point on P-256.
func ValidatePublicKey(raw []byte) error {
	if len(raw) != 65 || raw[0] != 0x04 {
		return ErrInvalidPublicKey
	}
	if _, err := ecdh.P256().NewPublicKey(raw); err != nil {
		return ErrInvalidPublicKey
	}
	return nil
}

type Device struct {
	DeviceID  string `json:"device_id"`
	PublicKey []byte `json:"public_key"`
	CreatedAt int64  `json:"created_at"`
}

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

// Register stores the public key of a device. Registering the same key again
// only marks the device as used (clients do it before every encrypted call); a
// different key for a known device is a conflict, because a key change must be
// visible to peers (revoke, then register a new device). A user at the device
// limit loses the device used least recently: browsers that were closed in
// private mode or cleared never revoke theirs.
func (r *Repository) Register(ctx context.Context, userID int64, deviceID string, publicKey []byte) error {
	if !ValidDeviceID(deviceID) {
		return ErrInvalidDevice
	}
	if err := ValidatePublicKey(publicKey); err != nil {
		return err
	}
	tx, err := storage.BeginWrite(ctx, r.db)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var existing []byte
	var revoked sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT public_key,revoked_at FROM e2ee_device_keys WHERE user_id=? AND device_id=?`, userID, deviceID).Scan(&existing, &revoked)
	switch {
	case err == nil && revoked.Valid:
		// A revoked device id is never reused: peers may still trust its key.
		return ErrKeyConflict
	case err == nil && string(existing) == string(publicKey):
		if _, err = tx.ExecContext(ctx, `UPDATE e2ee_device_keys SET last_used_at=unixepoch() WHERE user_id=? AND device_id=?`, userID, deviceID); err != nil {
			return fmt.Errorf("touch device key: %w", err)
		}
		return tx.Commit()
	case err == nil:
		return ErrKeyConflict
	case !errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("read device key: %w", err)
	}
	var active int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM e2ee_device_keys WHERE user_id=? AND revoked_at IS NULL`, userID).Scan(&active); err != nil {
		return fmt.Errorf("count devices: %w", err)
	}
	if active >= MaxActiveDevices {
		if _, err = tx.ExecContext(ctx, `UPDATE e2ee_device_keys SET revoked_at=unixepoch() WHERE user_id=? AND device_id IN (
 SELECT device_id FROM e2ee_device_keys WHERE user_id=? AND revoked_at IS NULL ORDER BY max(last_used_at,created_at),device_id LIMIT ?)`,
			userID, userID, active-MaxActiveDevices+1); err != nil {
			return fmt.Errorf("revoke least recently used device: %w", err)
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO e2ee_device_keys(user_id,device_id,public_key,last_used_at) VALUES(?,?,?,unixepoch())`, userID, deviceID, publicKey); err != nil {
		return fmt.Errorf("register device key: %w", err)
	}
	return tx.Commit()
}

// Revoke marks a device key as no longer usable.
func (r *Repository) Revoke(ctx context.Context, userID int64, deviceID string) error {
	if !ValidDeviceID(deviceID) {
		return ErrInvalidDevice
	}
	result, err := r.db.ExecContext(ctx, `UPDATE e2ee_device_keys SET revoked_at=unixepoch() WHERE user_id=? AND device_id=? AND revoked_at IS NULL`, userID, deviceID)
	if err != nil {
		return fmt.Errorf("revoke device key: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Devices returns the active keys of target for a viewer who shares a server
// or a conversation with them; strangers get ErrNotFound.
func (r *Repository) Devices(ctx context.Context, viewerID, targetID int64) ([]Device, error) {
	var visible bool
	err := r.db.QueryRowContext(ctx, `SELECT ?=? OR EXISTS(SELECT 1 FROM server_members a JOIN server_members b ON b.server_id=a.server_id WHERE a.user_id=? AND b.user_id=?)
 OR EXISTS(SELECT 1 FROM conversation_members a JOIN conversation_members b ON b.conversation_id=a.conversation_id WHERE a.user_id=? AND b.user_id=?)`,
		viewerID, targetID, viewerID, targetID, viewerID, targetID).Scan(&visible)
	if err != nil {
		return nil, fmt.Errorf("check device visibility: %w", err)
	}
	if !visible {
		return nil, ErrNotFound
	}
	rows, err := r.db.QueryContext(ctx, `SELECT k.device_id,k.public_key,k.created_at FROM e2ee_device_keys k JOIN users u ON u.id=k.user_id
 WHERE k.user_id=? AND k.revoked_at IS NULL AND u.deleted_at IS NULL ORDER BY k.device_id`, targetID)
	if err != nil {
		return nil, fmt.Errorf("list device keys: %w", err)
	}
	defer rows.Close()
	devices := []Device{}
	for rows.Next() {
		var d Device
		if err = rows.Scan(&d.DeviceID, &d.PublicKey, &d.CreatedAt); err != nil {
			return nil, err
		}
		devices = append(devices, d)
	}
	return devices, rows.Err()
}

// DeviceActive reports whether deviceID is a registered, non-revoked key of userID.
func (r *Repository) DeviceActive(ctx context.Context, userID int64, deviceID string) (bool, error) {
	if !ValidDeviceID(deviceID) {
		return false, nil
	}
	var active bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM e2ee_device_keys WHERE user_id=? AND device_id=? AND revoked_at IS NULL)`, userID, deviceID).Scan(&active)
	return active, err
}

// ChannelRequired reports whether a voice channel only accepts E2EE clients.
func (r *Repository) ChannelRequired(ctx context.Context, channelID int64) (bool, error) {
	var required bool
	err := r.db.QueryRowContext(ctx, `SELECT e2ee_required FROM channels WHERE id=?`, channelID).Scan(&required)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	return required, err
}

// ChannelPolicy reads the policy of a voice channel visible to a server member.
func (r *Repository) ChannelPolicy(ctx context.Context, serverID, channelID, userID int64) (bool, error) {
	var required bool
	err := r.db.QueryRowContext(ctx, `SELECT c.e2ee_required FROM channels c JOIN server_members m ON m.server_id=c.server_id AND m.user_id=?
 WHERE c.id=? AND c.server_id=? AND c.kind='voice'`, userID, channelID, serverID).Scan(&required)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	return required, err
}

// SetChannelPolicy changes the policy; only owners and administrators may.
// changed is false when the value was already set.
func (r *Repository) SetChannelPolicy(ctx context.Context, serverID, channelID, userID int64, required bool) (changed bool, err error) {
	tx, err := storage.BeginWrite(ctx, r.db)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var manager, current bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM server_members WHERE server_id=c.server_id AND user_id=? AND role IN ('owner','admin')),c.e2ee_required
 FROM channels c WHERE c.id=? AND c.server_id=? AND c.kind='voice'`, userID, channelID, serverID).Scan(&manager, &current)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	if !manager {
		return false, ErrForbidden
	}
	if current == required {
		return false, tx.Commit()
	}
	if _, err = tx.ExecContext(ctx, `UPDATE channels SET e2ee_required=? WHERE id=?`, required, channelID); err != nil {
		return false, fmt.Errorf("update channel encryption: %w", err)
	}
	return true, tx.Commit()
}

package sqlite

import (
	"errors"
	"voxhold-backend/internal/account"
	"context"
	"database/sql"
	"fmt"
)

type SessionRepository struct {
	db *sql.DB
}

func NewSessionRepository(db *sql.DB) *SessionRepository {
	return &SessionRepository{db: db}
}

// DeleteExpired limits each writer transaction and uses idx_sessions_expires_at.
// Expiration matches authentication: a session expiring at now is no longer active.
func (r *SessionRepository) DeleteExpired(ctx context.Context, now int64, limit int) (int64, error) {
	if limit < 1 || limit > 1000 {
		return 0, fmt.Errorf("session cleanup batch must be between 1 and 1000")
	}
	result, err := r.db.ExecContext(ctx, `DELETE FROM sessions WHERE id IN (
		SELECT id FROM sessions WHERE expires_at <= ? ORDER BY expires_at, id LIMIT ?
	)`, now, limit)
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	return result.RowsAffected()
}

func (r *SessionRepository) Create(
	ctx context.Context,
	userID int64,
	tokenHash []byte,
	expiresAt int64,
) error {
	const query = `
	INSERT INTO SESSIONS (
		user_id,
		token_hash,
		expires_at,
		user_agent,
		client_ip
	)
	VALUES(?, ?, ?, ?, ?)
	`

	meta := account.ClientMetaFromContext(ctx)
	_, err := r.db.ExecContext(
		ctx,
		query,
		userID,
		tokenHash,
		expiresAt,
		meta.UserAgent,
		meta.IP,
	)

	if err != nil {
		return fmt.Errorf("create session: %w", err)

	}

	return nil

}

func (r *SessionRepository) DeleteByTokenHash(
	ctx context.Context,
	tokenHash []byte,
) error {
	const query = `
	DELETE FROM sessions 
	WHERE token_hash = ?
	`

	_, err := r.db.ExecContext(ctx, query, tokenHash)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}

	return nil
}

func (r *SessionRepository) FindActiveUserIDByTokenHash(
	ctx context.Context,
	tokenHash []byte,
) (int64, error) {
	const query = `
	SELECT sessions.user_id
	FROM sessions
	JOIN users
	  ON users.id = sessions.user_id
	WHERE sessions.token_hash = ?
	  AND sessions.expires_at > unixepoch()
	  AND users.deleted_at IS NULL
	  AND NOT EXISTS (
		SELECT 1
		FROM user_bans
		WHERE user_bans.user_id = users.id
	  )
	`

	var userID int64

	err := r.db.QueryRowContext(ctx, query, tokenHash).Scan(&userID)
	if err != nil {
		return 0, fmt.Errorf("find active session: %w", err)
	}

	return userID, nil
}

func (r *SessionRepository) Rotate(
	ctx context.Context,
	oldTokenHash []byte,
	newTokenHash []byte,
	newExpiresAt int64,
) error {
	const query = `
	UPDATE sessions
	SET
		token_hash = ?,
		expires_at = ?,
		created_at = unixepoch()
	WHERE token_hash = ?
	  AND expires_at > unixepoch()
	  AND EXISTS (
		SELECT 1
		FROM users
		WHERE users.id = sessions.user_id
		  AND users.deleted_at IS NULL
		  AND NOT EXISTS (
			SELECT 1
			FROM user_bans
			WHERE user_bans.user_id = users.id
		  )
	  )
	RETURNING id
	`

	var sessionID int64

	err := r.db.QueryRowContext(
		ctx,
		query,
		newTokenHash,
		newExpiresAt,
		oldTokenHash,
	).Scan(&sessionID)
	if err != nil {
		return fmt.Errorf(
			"rotate session: %w",
			err,
		)
	}

	return nil
}

// Touch records activity at most every five minutes to avoid a write per request.
func (r *SessionRepository) Touch(ctx context.Context, tokenHash []byte, now int64) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE sessions SET last_used_at=? WHERE token_hash=? AND (last_used_at IS NULL OR last_used_at < ?)`,
		now, tokenHash, now-300)
	if err != nil {
		return fmt.Errorf("touch session: %w", err)
	}
	return nil
}

func (r *SessionRepository) ListByUser(ctx context.Context, userID int64) ([]account.SessionRecord, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, token_hash, created_at, last_used_at, expires_at,
		COALESCE(user_agent,''), COALESCE(client_ip,'')
		FROM sessions WHERE user_id=? AND expires_at > unixepoch() ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []account.SessionRecord
	for rows.Next() {
		var s account.SessionRecord
		var used sql.NullInt64
		if err = rows.Scan(&s.ID, &s.TokenHash, &s.CreatedAt, &used, &s.ExpiresAt, &s.UserAgent, &s.ClientIP); err != nil {
			return nil, err
		}
		if used.Valid {
			s.LastUsedAt = &used.Int64
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// DeleteByID removes one session owned by userID and returns its token hash.
func (r *SessionRepository) DeleteByID(ctx context.Context, userID, sessionID int64) ([]byte, error) {
	var hash []byte
	err := r.db.QueryRowContext(ctx, `DELETE FROM sessions WHERE id=? AND user_id=? RETURNING token_hash`, sessionID, userID).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, account.ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("delete session: %w", err)
	}
	return hash, nil
}

// DeleteOthers removes every session of userID except keepHash.
func (r *SessionRepository) DeleteOthers(ctx context.Context, userID int64, keepHash []byte) ([][]byte, error) {
	rows, err := r.db.QueryContext(ctx, `DELETE FROM sessions WHERE user_id=? AND token_hash<>? RETURNING token_hash`, userID, keepHash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hashes [][]byte
	for rows.Next() {
		var h []byte
		if err = rows.Scan(&h); err != nil {
			return nil, err
		}
		hashes = append(hashes, h)
	}
	return hashes, rows.Err()
}

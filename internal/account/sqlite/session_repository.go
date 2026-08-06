package sqlite

import (
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
		expires_at
	)
	VALUES(?, ?, ?)
	`

	_, err := r.db.ExecContext(
		ctx,
		query,
		userID,
		tokenHash,
		expiresAt,
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
	SELECT user_id FROM sessions
	WHERE token_hash = ?
		AND expires_at > unixepoch()
	`

	var userID int64

	err := r.db.QueryRowContext(ctx, query, tokenHash).Scan(&userID)
	if err != nil {
		return 0, fmt.Errorf("find active session: %w", err)
	}

	return userID, nil
}

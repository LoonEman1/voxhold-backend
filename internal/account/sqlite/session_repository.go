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

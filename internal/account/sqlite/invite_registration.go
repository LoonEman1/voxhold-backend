package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	sqliteDriver "modernc.org/sqlite"
	sqliteLib "modernc.org/sqlite/lib"

	"voxhold-backend/internal/account"
	"voxhold-backend/internal/server"
)

type InviteRegistrationRepository struct {
	db *sql.DB
}

func NewInviteRegistrationRepository(db *sql.DB) *InviteRegistrationRepository {
	return &InviteRegistrationRepository{db: db}
}

func (r *InviteRegistrationRepository) ValidateRegistrationInvite(
	ctx context.Context,
	inviteTokenHash []byte,
) error {
	const query = `
	SELECT 1
	FROM server_invite_links
	WHERE token_hash = ?
	  AND allow_registration = 1
	  AND revoked_at IS NULL
	  AND expires_at > unixepoch()
	  AND max_uses IS NOT NULL
	  AND use_count < max_uses
	`

	var valid int
	if err := r.db.QueryRowContext(ctx, query, inviteTokenHash).Scan(&valid); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return account.ErrRegistrationInviteInvalid
		}

		return fmt.Errorf("select registration invite: %w", err)
	}

	return nil
}

func (r *InviteRegistrationRepository) RegisterWithInvite(
	ctx context.Context,
	username string,
	passwordHash string,
	inviteTokenHash []byte,
	sessionTokenHash []byte,
	sessionExpiresAt int64,
) (account.UserInfo, int64, server.ServerMember, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return account.UserInfo{}, 0, server.ServerMember{}, fmt.Errorf(
			"begin invite registration transaction: %w",
			err,
		)
	}
	defer func() { _ = tx.Rollback() }()

	const consumeInviteQuery = `
	UPDATE server_invite_links
	SET use_count = use_count + 1
	WHERE token_hash = ?
	  AND allow_registration = 1
	  AND revoked_at IS NULL
	  AND expires_at > unixepoch()
	  AND max_uses IS NOT NULL
	  AND use_count < max_uses
	RETURNING server_id
	`

	var serverID int64
	if err := tx.QueryRowContext(
		ctx,
		consumeInviteQuery,
		inviteTokenHash,
	).Scan(&serverID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return account.UserInfo{}, 0, server.ServerMember{},
				account.ErrRegistrationInviteInvalid
		}

		return account.UserInfo{}, 0, server.ServerMember{}, fmt.Errorf(
			"consume registration invite: %w",
			err,
		)
	}

	const createUserQuery = `
	INSERT INTO users (username, password_hash)
	VALUES (?, ?)
	RETURNING id, username, created_at
	`

	var user account.UserInfo
	if err := tx.QueryRowContext(
		ctx,
		createUserQuery,
		username,
		passwordHash,
	).Scan(&user.ID, &user.Username, &user.CreatedAt); err != nil {
		var sqliteError *sqliteDriver.Error
		if errors.As(err, &sqliteError) &&
			sqliteError.Code() == sqliteLib.SQLITE_CONSTRAINT_UNIQUE {
			return account.UserInfo{}, 0, server.ServerMember{}, account.ErrUsernameTaken
		}

		return account.UserInfo{}, 0, server.ServerMember{}, fmt.Errorf(
			"create invited user: %w",
			err,
		)
	}

	const addMemberQuery = `
	INSERT INTO server_members (server_id, user_id, role)
	VALUES (?, ?, ?)
	RETURNING joined_at
	`

	member := server.ServerMember{
		UserID:    user.ID,
		Username:  user.Username,
		CreatedAt: user.CreatedAt,
		Role:      server.RoleMember,
		About:     "",
	}
	if err := tx.QueryRowContext(
		ctx,
		addMemberQuery,
		serverID,
		user.ID,
		server.RoleMember,
	).Scan(&member.JoinedAt); err != nil {
		return account.UserInfo{}, 0, server.ServerMember{}, fmt.Errorf(
			"add registered server member: %w",
			err,
		)
	}

	const createSessionQuery = `
	INSERT INTO sessions (user_id, token_hash, expires_at)
	VALUES (?, ?, ?)
	`

	if _, err := tx.ExecContext(
		ctx,
		createSessionQuery,
		user.ID,
		sessionTokenHash,
		sessionExpiresAt,
	); err != nil {
		return account.UserInfo{}, 0, server.ServerMember{}, fmt.Errorf(
			"create invited user session: %w",
			err,
		)
	}

	if err := tx.Commit(); err != nil {
		return account.UserInfo{}, 0, server.ServerMember{}, fmt.Errorf(
			"commit invite registration transaction: %w",
			err,
		)
	}

	return user, serverID, member, nil
}

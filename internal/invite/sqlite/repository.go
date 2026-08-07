package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	sqliteDriver "modernc.org/sqlite"
	sqliteLib "modernc.org/sqlite/lib"

	"voxhold-backend/internal/invite"
	"voxhold-backend/internal/server"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{
		db: db,
	}
}

func (r *Repository) CreateDirect(
	ctx context.Context,
	serverID int64,
	inviterUserID int64,
	inviteeUsername string,
	expiresAt int64,
) (invite.Invite, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return invite.Invite{}, fmt.Errorf(
			"begin create invite transaction: %w",
			err,
		)
	}

	defer func() {
		_ = tx.Rollback()
	}()

	const permissionQuery = `
	SELECT EXISTS (
		SELECT 1
		FROM server_members
		WHERE server_id = ?
		  AND user_id = ?
		  AND role IN (?, ?)
	)
	`

	var allowed bool

	err = tx.QueryRowContext(
		ctx,
		permissionQuery,
		serverID,
		inviterUserID,
		server.RoleOwner,
		server.RoleAdmin,
	).Scan(&allowed)
	if err != nil {
		return invite.Invite{}, fmt.Errorf(
			"check invite permission: %w",
			err,
		)
	}

	if !allowed {
		return invite.Invite{}, invite.ErrForbidden
	}

	const findUserQuery = `
	SELECT id
	FROM users
	WHERE username = ?
	`

	var inviteeUserID int64

	err = tx.QueryRowContext(
		ctx,
		findUserQuery,
		inviteeUsername,
	).Scan(&inviteeUserID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return invite.Invite{}, invite.ErrUserNotFound
		}

		return invite.Invite{}, fmt.Errorf(
			"find invitee: %w",
			err,
		)
	}

	if inviterUserID == inviteeUserID {
		return invite.Invite{}, invite.ErrCannotInviteSelf
	}

	const memberQuery = `
	SELECT EXISTS (
		SELECT 1
		FROM server_members
		WHERE server_id = ?
		  AND user_id = ?
	)
	`

	var alreadyMember bool

	err = tx.QueryRowContext(
		ctx,
		memberQuery,
		serverID,
		inviteeUserID,
	).Scan(&alreadyMember)
	if err != nil {
		return invite.Invite{}, fmt.Errorf(
			"check server membership: %w",
			err,
		)
	}

	if alreadyMember {
		return invite.Invite{}, invite.ErrAlreadyMember
	}

	const expireOldQuery = `
	UPDATE server_invites
	SET status = ?
	WHERE server_id = ?
	  AND invitee_user_id = ?
	  AND status = ?
	  AND expires_at <= unixepoch()
	`

	_, err = tx.ExecContext(
		ctx,
		expireOldQuery,
		invite.StatusExpired,
		serverID,
		inviteeUserID,
		invite.StatusPending,
	)
	if err != nil {
		return invite.Invite{}, fmt.Errorf(
			"expire old invitation: %w",
			err,
		)
	}

	const insertQuery = `
	INSERT INTO server_invites (
		server_id,
		inviter_user_id,
		invitee_user_id,
		status,
		expires_at
	)
	VALUES (?, ?, ?, ?, ?)
	RETURNING
		id,
		server_id,
		inviter_user_id,
		invitee_user_id,
		status,
		expires_at,
		responded_at,
		created_at
	`

	var createdInvite invite.Invite

	err = tx.QueryRowContext(
		ctx,
		insertQuery,
		serverID,
		inviterUserID,
		inviteeUserID,
		invite.StatusPending,
		expiresAt,
	).Scan(
		&createdInvite.ID,
		&createdInvite.ServerID,
		&createdInvite.InviterUserID,
		&createdInvite.InviteeUserID,
		&createdInvite.Status,
		&createdInvite.ExpiresAt,
		&createdInvite.RespondedAt,
		&createdInvite.CreatedAt,
	)
	if err != nil {
		var sqliteError *sqliteDriver.Error

		if errors.As(err, &sqliteError) &&
			sqliteError.Code() == sqliteLib.SQLITE_CONSTRAINT_UNIQUE {

			return invite.Invite{}, invite.ErrInviteAlreadyPending
		}

		return invite.Invite{}, fmt.Errorf(
			"insert invitation: %w",
			err,
		)
	}

	if err := tx.Commit(); err != nil {
		return invite.Invite{}, fmt.Errorf(
			"commit create invite transaction: %w",
			err,
		)
	}

	return createdInvite, nil
}

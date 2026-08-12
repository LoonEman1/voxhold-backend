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
	SELECT
		servers.name,
		users.username
	FROM server_members
	JOIN servers
		ON servers.id = server_members.server_id
	JOIN users
		ON users.id = server_members.user_id
	WHERE server_members.server_id = ?
	  AND server_members.user_id = ?
	  AND server_members.role IN (?, ?)
	`

	var serverName string
	var inviterUsername string

	err = tx.QueryRowContext(
		ctx,
		permissionQuery,
		serverID,
		inviterUserID,
		server.RoleOwner,
		server.RoleAdmin,
	).Scan(
		&serverName,
		&inviterUsername,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return invite.Invite{}, invite.ErrForbidden
		}

		return invite.Invite{}, fmt.Errorf(
			"check invite permission: %w",
			err,
		)
	}

	const findUserQuery = `
	SELECT id
	FROM users
	WHERE username = ?
	  AND deleted_at IS NULL
	  AND NOT EXISTS (
		SELECT 1
		FROM user_bans
		WHERE user_bans.user_id = users.id
	  )
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

	createdInvite.ServerName = serverName
	createdInvite.InviterUsername = inviterUsername

	return createdInvite, nil
}

func (r *Repository) ListIncoming(
	ctx context.Context,
	inviteeUserID int64,
) ([]invite.IncomingInvite, error) {
	const query = `
	SELECT
		server_invites.id,
		server_invites.server_id,
		servers.name,
		server_invites.inviter_user_id,
		inviter.username,
		server_invites.status,
		server_invites.expires_at,
		server_invites.created_at
	FROM server_invites
	JOIN servers
		ON servers.id = server_invites.server_id
	JOIN users AS inviter
		ON inviter.id = server_invites.inviter_user_id
	WHERE server_invites.invitee_user_id = ?
	  AND server_invites.status = ?
	  AND server_invites.expires_at > unixepoch()
	ORDER BY
		server_invites.created_at DESC,
		server_invites.id DESC
	`

	rows, err := r.db.QueryContext(
		ctx,
		query,
		inviteeUserID,
		invite.StatusPending,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"select incoming invitations: %w",
			err,
		)
	}
	defer rows.Close()

	invitations := make([]invite.IncomingInvite, 0)

	for rows.Next() {
		var incomingInvite invite.IncomingInvite

		if err := rows.Scan(
			&incomingInvite.ID,
			&incomingInvite.ServerID,
			&incomingInvite.ServerName,
			&incomingInvite.InviterUserID,
			&incomingInvite.InviterUsername,
			&incomingInvite.Status,
			&incomingInvite.ExpiresAt,
			&incomingInvite.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf(
				"scan incoming invitation: %w",
				err,
			)
		}

		invitations = append(
			invitations,
			incomingInvite,
		)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf(
			"iterate incoming invitations: %w",
			err,
		)
	}

	return invitations, nil
}
func (r *Repository) Accept(
	ctx context.Context,
	inviteID int64,
	inviteeUserID int64,
) (int64, server.ServerMember, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, server.ServerMember{}, fmt.Errorf(
			"begin accept invitation transaction: %w",
			err,
		)
	}

	defer func() {
		_ = tx.Rollback()
	}()

	const acceptQuery = `
	UPDATE server_invites
	SET
		status = ?,
		responded_at = unixepoch()
	WHERE id = ?
	  AND invitee_user_id = ?
	  AND status = ?
	  AND expires_at > unixepoch()
	RETURNING server_id
	`

	var serverID int64

	err = tx.QueryRowContext(
		ctx,
		acceptQuery,
		invite.StatusAccepted,
		inviteID,
		inviteeUserID,
		invite.StatusPending,
	).Scan(&serverID)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, server.ServerMember{}, fmt.Errorf(
				"accept invitation: %w",
				err,
			)
		}

		const stateQuery = `
		SELECT
			status,
			expires_at <= unixepoch()
		FROM server_invites
		WHERE id = ?
		  AND invitee_user_id = ?
		`

		var status invite.Status
		var expired bool

		err = tx.QueryRowContext(
			ctx,
			stateQuery,
			inviteID,
			inviteeUserID,
		).Scan(
			&status,
			&expired,
		)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return 0, server.ServerMember{},
					invite.ErrInviteNotFound
			}

			return 0, server.ServerMember{}, fmt.Errorf(
				"get invitation state: %w",
				err,
			)
		}

		if status == invite.StatusExpired {
			return 0, server.ServerMember{},
				invite.ErrInviteExpired
		}

		if status != invite.StatusPending {
			return 0, server.ServerMember{},
				invite.ErrInviteNotPending
		}

		if expired {
			const expireQuery = `
			UPDATE server_invites
			SET status = ?
			WHERE id = ?
			  AND status = ?
			`

			_, err = tx.ExecContext(
				ctx,
				expireQuery,
				invite.StatusExpired,
				inviteID,
				invite.StatusPending,
			)
			if err != nil {
				return 0, server.ServerMember{}, fmt.Errorf(
					"expire invitation: %w",
					err,
				)
			}

			if err := tx.Commit(); err != nil {
				return 0, server.ServerMember{}, fmt.Errorf(
					"commit expired invitation: %w",
					err,
				)
			}

			return 0, server.ServerMember{},
				invite.ErrInviteExpired
		}

		return 0, server.ServerMember{},
			invite.ErrInviteNotPending
	}

	const addMemberQuery = `
	INSERT INTO server_members (
		server_id,
		user_id,
		role
	)
	VALUES (?, ?, ?)
	ON CONFLICT (server_id, user_id) DO NOTHING
	`

	_, err = tx.ExecContext(
		ctx,
		addMemberQuery,
		serverID,
		inviteeUserID,
		server.RoleMember,
	)
	if err != nil {
		return 0, server.ServerMember{}, fmt.Errorf(
			"add invited server member: %w",
			err,
		)
	}

	acceptedMember, err := selectAcceptedMember(
		ctx,
		tx,
		serverID,
		inviteeUserID,
	)
	if err != nil {
		return 0, server.ServerMember{}, err
	}

	if err := tx.Commit(); err != nil {
		return 0, server.ServerMember{}, fmt.Errorf(
			"commit accept invitation transaction: %w",
			err,
		)
	}

	return serverID, acceptedMember, nil
}

func selectAcceptedMember(
	ctx context.Context,
	tx *sql.Tx,
	serverID int64,
	userID int64,
) (server.ServerMember, error) {
	const query = `
	SELECT
		server_members.user_id,
		users.username,
		users.created_at,
		server_members.role,
		server_members.joined_at,
		COALESCE(user_profiles.about, ''),
		user_profiles.country_code,
		user_profiles.last_seen_at
	FROM server_members
	JOIN users
		ON users.id = server_members.user_id
	LEFT JOIN user_profiles
		ON user_profiles.user_id = server_members.user_id
	WHERE server_members.server_id = ?
	  AND server_members.user_id = ?
	`

	var member server.ServerMember

	if err := tx.QueryRowContext(
		ctx, query, serverID, userID,
	).Scan(
		&member.UserID,
		&member.Username,
		&member.CreatedAt,
		&member.Role,
		&member.JoinedAt,
		&member.About,
		&member.CountryCode,
		&member.LastSeenAt,
	); err != nil {
		return server.ServerMember{}, fmt.Errorf(
			"select accepted server member: %w",
			err,
		)
	}

	return member, nil
}

func (r *Repository) Decline(
	ctx context.Context,
	inviteID int64,
	inviteeUserID int64,
) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf(
			"begin decline invitation transaction: %w",
			err,
		)
	}

	defer func() {
		_ = tx.Rollback()
	}()

	const declineQuery = `
	UPDATE server_invites
	SET
		status = ?,
		responded_at = unixepoch()
	WHERE id = ?
	  AND invitee_user_id = ?
	  AND status = ?
	  AND expires_at > unixepoch()
	RETURNING id
	`

	var declinedInviteID int64

	err = tx.QueryRowContext(
		ctx,
		declineQuery,
		invite.StatusDeclined,
		inviteID,
		inviteeUserID,
		invite.StatusPending,
	).Scan(&declinedInviteID)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf(
				"decline invitation: %w",
				err,
			)
		}

		const stateQuery = `
		SELECT
			status,
			expires_at <= unixepoch()
		FROM server_invites
		WHERE id = ?
		  AND invitee_user_id = ?
		`

		var status invite.Status
		var expired bool

		err = tx.QueryRowContext(
			ctx,
			stateQuery,
			inviteID,
			inviteeUserID,
		).Scan(
			&status,
			&expired,
		)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return invite.ErrInviteNotFound
			}

			return fmt.Errorf(
				"get invitation state: %w",
				err,
			)
		}

		if status == invite.StatusExpired {
			return invite.ErrInviteExpired
		}

		if status != invite.StatusPending {
			return invite.ErrInviteNotPending
		}

		if expired {
			const expireQuery = `
			UPDATE server_invites
			SET status = ?
			WHERE id = ?
			  AND status = ?
			`

			_, err = tx.ExecContext(
				ctx,
				expireQuery,
				invite.StatusExpired,
				inviteID,
				invite.StatusPending,
			)
			if err != nil {
				return fmt.Errorf(
					"expire invitation: %w",
					err,
				)
			}

			if err := tx.Commit(); err != nil {
				return fmt.Errorf(
					"commit expired invitation: %w",
					err,
				)
			}

			return invite.ErrInviteExpired
		}

		return invite.ErrInviteNotPending
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf(
			"commit decline invitation transaction: %w",
			err,
		)
	}

	return nil
}

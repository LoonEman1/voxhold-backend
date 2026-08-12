package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"voxhold-backend/internal/server"
)

func (r *Repository) UpdateMemberRole(
	ctx context.Context,
	serverID int64,
	requesterUserID int64,
	targetUserID int64,
	role server.Role,
) (server.ServerMember, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return server.ServerMember{}, false, fmt.Errorf(
			"begin update member role transaction: %w",
			err,
		)
	}
	defer func() { _ = tx.Rollback() }()

	requesterRole, err := findMemberRole(
		ctx, tx, serverID, requesterUserID,
	)
	if err != nil {
		if errors.Is(err, server.ErrMemberNotFound) {
			return server.ServerMember{}, false,
				server.ErrManageMembersForbidden
		}

		return server.ServerMember{}, false, err
	}

	if requesterRole != server.RoleOwner {
		return server.ServerMember{}, false,
			server.ErrManageMembersForbidden
	}

	if requesterUserID == targetUserID {
		return server.ServerMember{}, false,
			server.ErrCannotChangeOwnRole
	}

	targetRole, err := findMemberRole(
		ctx, tx, serverID, targetUserID,
	)
	if err != nil {
		return server.ServerMember{}, false, err
	}

	if targetRole == server.RoleOwner {
		return server.ServerMember{}, false,
			server.ErrOwnerRoleImmutable
	}

	if targetRole == role {
		member, err := findServerMember(
			ctx, tx, serverID, targetUserID,
		)
		if err != nil {
			return server.ServerMember{}, false, err
		}

		if err := tx.Commit(); err != nil {
			return server.ServerMember{}, false, fmt.Errorf(
				"commit unchanged member role transaction: %w",
				err,
			)
		}

		return member, false, nil
	}

	const updateQuery = `
	UPDATE server_members
	SET role = ?
	WHERE server_id = ?
	  AND user_id = ?
	  AND role = ?
	`

	result, err := tx.ExecContext(
		ctx,
		updateQuery,
		role,
		serverID,
		targetUserID,
		targetRole,
	)
	if err != nil {
		return server.ServerMember{}, false, fmt.Errorf(
			"update member role row: %w",
			err,
		)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return server.ServerMember{}, false, fmt.Errorf(
			"count updated member roles: %w",
			err,
		)
	}
	if affected != 1 {
		return server.ServerMember{}, false,
			server.ErrMemberNotFound
	}

	member, err := findServerMember(
		ctx, tx, serverID, targetUserID,
	)
	if err != nil {
		return server.ServerMember{}, false, err
	}

	if err := tx.Commit(); err != nil {
		return server.ServerMember{}, false, fmt.Errorf(
			"commit update member role transaction: %w",
			err,
		)
	}

	return member, true, nil
}

func (r *Repository) BanMember(
	ctx context.Context,
	serverID int64,
	requesterUserID int64,
	targetUserID int64,
) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf(
			"begin ban member transaction: %w",
			err,
		)
	}
	defer func() { _ = tx.Rollback() }()

	requesterRole, err := findMemberRole(
		ctx, tx, serverID, requesterUserID,
	)
	if err != nil {
		if errors.Is(err, server.ErrMemberNotFound) {
			return server.ErrManageMembersForbidden
		}

		return err
	}

	if requesterUserID == targetUserID {
		return server.ErrCannotBanSelf
	}

	targetRole, err := findMemberRole(
		ctx, tx, serverID, targetUserID,
	)
	if err != nil {
		return err
	}

	if targetRole == server.RoleOwner {
		return server.ErrOwnerCannotBeBanned
	}

	allowed := requesterRole == server.RoleOwner ||
		(requesterRole == server.RoleAdmin &&
			targetRole == server.RoleMember)
	if !allowed {
		return server.ErrManageMembersForbidden
	}

	const deleteReadsQuery = `
	DELETE FROM channel_reads
	WHERE user_id = ?
	  AND channel_id IN (
		SELECT id
		FROM channels
		WHERE server_id = ?
	  )
	`

	if _, err := tx.ExecContext(
		ctx,
		deleteReadsQuery,
		targetUserID,
		serverID,
	); err != nil {
		return fmt.Errorf(
			"delete banned member read states: %w",
			err,
		)
	}

	const banQuery = `
	INSERT INTO user_bans (user_id, banned_by_user_id)
	VALUES (?, ?)
	`

	if _, err := tx.ExecContext(
		ctx,
		banQuery,
		targetUserID,
		requesterUserID,
	); err != nil {
		return fmt.Errorf(
			"create instance ban: %w",
			err,
		)
	}

	const revokeQuery = `
	DELETE FROM sessions
	WHERE user_id = ?;

	DELETE FROM server_invites
	WHERE inviter_user_id = ?
	   OR invitee_user_id = ?;

	DELETE FROM server_invite_links
	WHERE created_by = ?;

	DELETE FROM server_members
	WHERE user_id = ?;
	`

	if _, err := tx.ExecContext(
		ctx,
		revokeQuery,
		targetUserID,
		targetUserID,
		targetUserID,
		targetUserID,
		targetUserID,
	); err != nil {
		return fmt.Errorf("revoke banned account access: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf(
			"commit ban member transaction: %w",
			err,
		)
	}

	return nil
}

func findMemberRole(
	ctx context.Context,
	tx *sql.Tx,
	serverID int64,
	userID int64,
) (server.Role, error) {
	const query = `
	SELECT role
	FROM server_members
	WHERE server_id = ?
	  AND user_id = ?
	`

	var role server.Role

	if err := tx.QueryRowContext(
		ctx, query, serverID, userID,
	).Scan(&role); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", server.ErrMemberNotFound
		}

		return "", fmt.Errorf(
			"find server member role: %w",
			err,
		)
	}

	return role, nil
}

func findServerMember(
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
		if errors.Is(err, sql.ErrNoRows) {
			return server.ServerMember{}, server.ErrMemberNotFound
		}

		return server.ServerMember{}, fmt.Errorf(
			"find server member: %w",
			err,
		)
	}

	return member, nil
}

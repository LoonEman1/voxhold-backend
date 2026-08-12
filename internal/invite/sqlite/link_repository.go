package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"voxhold-backend/internal/invite"
	"voxhold-backend/internal/server"
)

func (r *Repository) CreateLink(
	ctx context.Context,
	serverID int64,
	creatorUserID int64,
	tokenHash []byte,
	expiresAt int64,
	maxUses *int,
	allowRegistration bool,
) (invite.InviteLink, error) {
	const query = `
	INSERT INTO server_invite_links (
		server_id,
		created_by,
		token_hash,
		expires_at,
		max_uses,
		allow_registration
	)
	SELECT ?, ?, ?, ?, ?, ?
	FROM server_members
	WHERE server_id = ?
	  AND user_id = ?
	  AND role IN (?, ?)
	RETURNING
		id,
		server_id,
		(SELECT name FROM servers WHERE id = server_id),
		created_by,
		(SELECT username FROM users WHERE id = created_by),
		expires_at,
		max_uses,
		use_count,
		allow_registration,
		created_at
	`

	var createdLink invite.InviteLink
	err := r.db.QueryRowContext(
		ctx,
		query,
		serverID,
		creatorUserID,
		tokenHash,
		expiresAt,
		maxUses,
		allowRegistration,
		serverID,
		creatorUserID,
		server.RoleOwner,
		server.RoleAdmin,
	).Scan(
		&createdLink.ID,
		&createdLink.ServerID,
		&createdLink.ServerName,
		&createdLink.CreatedBy,
		&createdLink.CreatorUsername,
		&createdLink.ExpiresAt,
		&createdLink.MaxUses,
		&createdLink.UseCount,
		&createdLink.AllowRegistration,
		&createdLink.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return invite.InviteLink{}, invite.ErrForbidden
		}

		return invite.InviteLink{}, fmt.Errorf("insert invite link: %w", err)
	}

	return createdLink, nil
}

func (r *Repository) ResolveLink(
	ctx context.Context,
	tokenHash []byte,
) (invite.LinkPreview, error) {
	const query = `
	SELECT
		server_invite_links.server_id,
		servers.name,
		users.username,
		server_invite_links.expires_at,
		server_invite_links.max_uses,
		server_invite_links.use_count,
		server_invite_links.allow_registration
	FROM server_invite_links
	JOIN servers
		ON servers.id = server_invite_links.server_id
	JOIN users
		ON users.id = server_invite_links.created_by
	WHERE server_invite_links.token_hash = ?
	  AND server_invite_links.revoked_at IS NULL
	  AND server_invite_links.expires_at > unixepoch()
	  AND (
		server_invite_links.max_uses IS NULL
		OR server_invite_links.use_count < server_invite_links.max_uses
	  )
	`

	var preview invite.LinkPreview
	err := r.db.QueryRowContext(ctx, query, tokenHash).Scan(
		&preview.ServerID,
		&preview.ServerName,
		&preview.CreatorUsername,
		&preview.ExpiresAt,
		&preview.MaxUses,
		&preview.UseCount,
		&preview.AllowRegistration,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return invite.LinkPreview{}, invite.ErrLinkInvalid
		}

		return invite.LinkPreview{}, fmt.Errorf("select invite link: %w", err)
	}

	return preview, nil
}

func (r *Repository) AcceptLink(
	ctx context.Context,
	tokenHash []byte,
	userID int64,
) (int64, server.ServerMember, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, server.ServerMember{}, false, fmt.Errorf(
			"begin accept invite link transaction: %w",
			err,
		)
	}
	defer func() { _ = tx.Rollback() }()

	const consumeQuery = `
	UPDATE server_invite_links
	SET use_count = use_count + 1
	WHERE token_hash = ?
	  AND revoked_at IS NULL
	  AND expires_at > unixepoch()
	  AND (max_uses IS NULL OR use_count < max_uses)
	  AND NOT EXISTS (
		SELECT 1
		FROM server_members
		WHERE server_members.server_id = server_invite_links.server_id
		  AND server_members.user_id = ?
	  )
	RETURNING server_id
	`

	var consumedServerID int64
	if err := tx.QueryRowContext(
		ctx,
		consumeQuery,
		tokenHash,
		userID,
	).Scan(&consumedServerID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			const existingMemberQuery = `
			SELECT server_invite_links.server_id
			FROM server_invite_links
			JOIN server_members
			  ON server_members.server_id = server_invite_links.server_id
			 AND server_members.user_id = ?
			WHERE server_invite_links.token_hash = ?
			`

			var serverID int64
			if err := tx.QueryRowContext(
				ctx,
				existingMemberQuery,
				userID,
				tokenHash,
			).Scan(&serverID); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return 0, server.ServerMember{}, false, invite.ErrLinkInvalid
				}

				return 0, server.ServerMember{}, false, fmt.Errorf(
					"select existing invite link member: %w",
					err,
				)
			}

			member, err := selectAcceptedMember(ctx, tx, serverID, userID)
			if err != nil {
				return 0, server.ServerMember{}, false, err
			}

			if err := tx.Commit(); err != nil {
				return 0, server.ServerMember{}, false, fmt.Errorf(
					"commit existing invite link membership: %w",
					err,
				)
			}

			return serverID, member, true, nil
		}

		return 0, server.ServerMember{}, false, fmt.Errorf(
			"consume invite link: %w",
			err,
		)
	}

	const addMemberQuery = `
	INSERT INTO server_members (server_id, user_id, role)
	VALUES (?, ?, ?)
	ON CONFLICT (server_id, user_id) DO NOTHING
	`

	if _, err := tx.ExecContext(
		ctx,
		addMemberQuery,
		consumedServerID,
		userID,
		server.RoleMember,
	); err != nil {
		return 0, server.ServerMember{}, false, fmt.Errorf(
			"add invite link server member: %w",
			err,
		)
	}

	member, err := selectAcceptedMember(ctx, tx, consumedServerID, userID)
	if err != nil {
		return 0, server.ServerMember{}, false, err
	}

	if err := tx.Commit(); err != nil {
		return 0, server.ServerMember{}, false, fmt.Errorf(
			"commit accept invite link transaction: %w",
			err,
		)
	}

	return consumedServerID, member, false, nil
}

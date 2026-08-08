package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"voxhold-backend/internal/server"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Create(
	ctx context.Context,
	name string,
	createdBy int64,
) (server.Server, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return server.Server{}, fmt.Errorf(
			"begin create server transaction: %w",
			err,
		)
	}

	defer func() {
		_ = tx.Rollback()
	}()

	const createServerQuery = `
	INSERT INTO servers (
		name,
		created_by
	)
	VALUES (?, ?)
	RETURNING
		id,
		name,
		created_by,
		created_at
	`

	var createdServer server.Server

	err = tx.QueryRowContext(
		ctx,
		createServerQuery,
		name,
		createdBy,
	).Scan(
		&createdServer.ID,
		&createdServer.Name,
		&createdServer.CreatedBy,
		&createdServer.CreatedAt,
	)

	if err != nil {
		return server.Server{}, fmt.Errorf(
			"insert server: %w",
			err,
		)
	}

	const addOwnerQuery = `
	INSERT INTO server_members(
		server_id,
		user_id,
		role
	)
	VALUES (?, ?, ?)
	`

	_, err = tx.ExecContext(
		ctx,
		addOwnerQuery,
		createdServer.ID,
		createdBy,
		server.RoleOwner,
	)
	if err != nil {
		return server.Server{}, fmt.Errorf(
			"add server owner: %w",
			err,
		)
	}

	if err := tx.Commit(); err != nil {
		return server.Server{}, fmt.Errorf(
			"commit create server transaction: %w",
			err,
		)
	}

	return createdServer, nil
}

func (r *Repository) Update(
	ctx context.Context,
	serverID int64,
	userID int64,
	name string,
) (server.Server, error) {
	const query = `
	UPDATE servers
	SET name = ?
	WHERE id = ?
		AND EXISTS(
		SELECT 1
		FROM server_members
		WHERE server_members.server_id = servers.id
		  AND server_members.user_id = ?
		  AND server_members.role = ?
	)
	RETURNING
		id,
		name,
		created_by,
		created_at
	`

	var updatedServer server.Server
	err := r.db.QueryRowContext(
		ctx,
		query,
		name,
		serverID,
		userID,
		server.RoleOwner,
	).Scan(
		&updatedServer.ID,
		&updatedServer.Name,
		&updatedServer.CreatedBy,
		&updatedServer.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return server.Server{}, server.ErrNotFound
		}

		return server.Server{}, fmt.Errorf(
			"update server: %w",
			err,
		)
	}

	return updatedServer, nil
}

func (r *Repository) Delete(
	ctx context.Context,
	serverID int64,
	userID int64,
) error {
	const query = `
	DELETE FROM servers
	WHERE id = ?
		AND EXISTS (
		SELECT 1
		FROM server_members
		WHERE server_members.server_id = ?
			AND server_members.user_id = ?
			AND server_members.role = ?
		)
	`

	result, err := r.db.ExecContext(
		ctx,
		query,
		serverID,
		serverID,
		userID,
		server.RoleOwner,
	)

	if err != nil {
		return fmt.Errorf("delete server: %w", err)
	}

	affectedRows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf(
			"get deleted server count: %w",
			err,
		)
	}

	if affectedRows == 0 {
		return server.ErrNotFound
	}

	return nil
}

func (r *Repository) Leave(
	ctx context.Context,
	serverID int64,
	userID int64,
) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf(
			"begin leave server transaction: %w",
			err,
		)
	}

	defer func() {
		_ = tx.Rollback()
	}()

	const roleQuery = `
	SELECT role
	FROM server_members
	WHERE server_id = ?
	  AND user_id = ?
	`

	var role server.Role

	err = tx.QueryRowContext(
		ctx,
		roleQuery,
		serverID,
		userID,
	).Scan(&role)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return server.ErrMembershipNotFound
		}

		return fmt.Errorf(
			"find server membership: %w",
			err,
		)
	}

	if role == server.RoleOwner {
		return server.ErrOwnerCannotLeave
	}

	const deleteMembershipQuery = `
	DELETE FROM server_members
	WHERE server_id = ?
	  AND user_id = ?
	  AND role <> ?
	`

	result, err := tx.ExecContext(
		ctx,
		deleteMembershipQuery,
		serverID,
		userID,
		server.RoleOwner,
	)
	if err != nil {
		return fmt.Errorf(
			"delete server membership: %w",
			err,
		)
	}

	affectedRows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf(
			"get deleted membership count: %w",
			err,
		)
	}

	if affectedRows == 0 {
		return server.ErrMembershipNotFound
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf(
			"commit leave server transaction: %w",
			err,
		)
	}

	return nil
}

func (r *Repository) ListByUserID(
	ctx context.Context,
	userID int64,
) ([]server.JoinedServer, error) {
	const query = `
	SELECT
		servers.id,
		servers.name,
		servers.created_by,
		servers.created_at,
		server_members.role,
		server_members.joined_at
	FROM server_members
	JOIN servers
		ON servers.id = server_members.server_id
	WHERE server_members.user_id = ?
	ORDER BY
		server_members.joined_at ASC,
		servers.id ASC
	`

	rows, err := r.db.QueryContext(
		ctx,
		query,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"query user servers: %w",
			err,
		)
	}
	defer rows.Close()

	joinedServers := make([]server.JoinedServer, 0)

	for rows.Next() {
		var joinedServer server.JoinedServer

		if err := rows.Scan(
			&joinedServer.ID,
			&joinedServer.Name,
			&joinedServer.CreatedBy,
			&joinedServer.CreatedAt,
			&joinedServer.Role,
			&joinedServer.JoinedAt,
		); err != nil {
			return nil, fmt.Errorf(
				"scan user server: %w",
				err,
			)
		}

		joinedServers = append(
			joinedServers,
			joinedServer,
		)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf(
			"iterate user servers: %w",
			err,
		)
	}

	return joinedServers, nil
}

func (r *Repository) ListMembers(
	ctx context.Context,
	serverID int64,
	requesterUserID int64,
) ([]server.ServerMember, error) {
	tx, err := r.db.BeginTx(
		ctx,
		&sql.TxOptions{
			ReadOnly: true,
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"begin list members transaction: %w",
			err,
		)
	}

	defer func() {
		_ = tx.Rollback()
	}()

	const membershipQuery = `
	SELECT EXISTS (
		SELECT 1
		FROM server_members
		WHERE server_id = ?
		  AND user_id = ?
	)
	`

	var requesterIsMember bool

	if err := tx.QueryRowContext(
		ctx,
		membershipQuery,
		serverID,
		requesterUserID,
	).Scan(&requesterIsMember); err != nil {
		return nil, fmt.Errorf(
			"check requester membership: %w",
			err,
		)
	}

	if !requesterIsMember {
		return nil, server.ErrMembersForbidden
	}

	const membersQuery = `
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
	ORDER BY
		CASE server_members.role
			WHEN 'owner' THEN 0
			WHEN 'admin' THEN 1
			ELSE 2
		END,
		users.username ASC,
		users.id ASC
	`

	rows, err := tx.QueryContext(
		ctx,
		membersQuery,
		serverID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"query server members: %w",
			err,
		)
	}
	defer rows.Close()

	members := make([]server.ServerMember, 0)

	for rows.Next() {
		var member server.ServerMember

		if err := rows.Scan(
			&member.UserID,
			&member.Username,
			&member.CreatedAt,
			&member.Role,
			&member.JoinedAt,
			&member.About,
			&member.CountryCode,
			&member.LastSeenAt,
		); err != nil {
			return nil, fmt.Errorf(
				"scan server member: %w",
				err,
			)
		}

		members = append(members, member)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf(
			"iterate server members: %w",
			err,
		)
	}

	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf(
			"close server member rows: %w",
			err,
		)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf(
			"commit list members transaction: %w",
			err,
		)
	}

	return members, nil
}

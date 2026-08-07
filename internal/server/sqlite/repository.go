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
	INSERT INTO servers(
	name, created_by
	)
	SELECT ?, ?
	WHERE NOT EXISTS(
		SELECT 1
		FROM servers
	)
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
		if errors.Is(err, sql.ErrNoRows) {
			return server.Server{}, server.ErrAlreadyExists
		}

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

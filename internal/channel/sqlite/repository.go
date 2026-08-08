package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"modernc.org/sqlite"
	sqliteLib "modernc.org/sqlite/lib"

	"voxhold-backend/internal/channel"
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

func (r *Repository) Create(
	ctx context.Context,
	serverID int64,
	userID int64,
	name string,
	kind channel.Kind,
) (channel.Channel, error) {

	const query = `
	INSERT INTO channels (
		server_id,
		name,
		kind,
		position,
		created_by
	)
	SELECT
		?,
		?,
		?,
		COALESCE((
			SELECT MAX(position) + 1
			FROM channels
			WHERE server_id = ?
		), 0),
		?
	WHERE EXISTS (
		SELECT 1
		FROM server_members
		WHERE server_id = ?
		  AND user_id = ?
		  AND role IN (?, ?)
	)
	RETURNING
		id,
		server_id,
		name,
		kind,
		position,
		created_by,
		created_at
	`

	var createdChannel channel.Channel

	err := r.db.QueryRowContext(
		ctx,
		query,
		serverID,
		name,
		kind,
		serverID,
		userID,
		serverID,
		userID,
		server.RoleOwner,
		server.RoleAdmin,
	).Scan(
		&createdChannel.ID,
		&createdChannel.ServerID,
		&createdChannel.Name,
		&createdChannel.Kind,
		&createdChannel.Position,
		&createdChannel.CreatedBy,
		&createdChannel.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return channel.Channel{}, channel.ErrForbidden
		}

		var sqliteError *sqlite.Error
		if errors.As(err, &sqliteError) &&
			sqliteError.Code() == sqliteLib.SQLITE_CONSTRAINT_UNIQUE {

			return channel.Channel{}, channel.ErrNameAlreadyExists
		}

		return channel.Channel{}, fmt.Errorf(
			"insert channel: %w",
			err,
		)
	}

	return createdChannel, nil
}

func (r *Repository) ListByServerID(
	ctx context.Context,
	serverID int64,
	userID int64,
) ([]channel.Channel, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf(
			"begin list channels transaction: %w",
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

	var isMember bool

	if err := tx.QueryRowContext(
		ctx,
		membershipQuery,
		serverID,
		userID,
	).Scan(&isMember); err != nil {
		return nil, fmt.Errorf(
			"check server membership: %w",
			err,
		)
	}

	if !isMember {
		return nil, channel.ErrForbidden
	}

	const channelsQuery = `
	SELECT
		id,
		server_id,
		name,
		kind,
		position,
		created_by,
		created_at
	FROM channels
	WHERE server_id = ?
	ORDER BY
		position ASC,
		id ASC
	`

	rows, err := tx.QueryContext(
		ctx,
		channelsQuery,
		serverID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"query server channels: %w",
			err,
		)
	}
	defer rows.Close()

	channels := make([]channel.Channel, 0)

	for rows.Next() {
		var value channel.Channel

		if err := rows.Scan(
			&value.ID,
			&value.ServerID,
			&value.Name,
			&value.Kind,
			&value.Position,
			&value.CreatedBy,
			&value.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf(
				"scan server channel: %w",
				err,
			)
		}

		channels = append(channels, value)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf(
			"iterate server channels: %w",
			err,
		)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf(
			"commit list channels transaction: %w",
			err,
		)
	}

	return channels, nil
}

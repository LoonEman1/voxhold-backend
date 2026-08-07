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

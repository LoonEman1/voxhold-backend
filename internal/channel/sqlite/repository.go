package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"modernc.org/sqlite"
	sqliteLib "modernc.org/sqlite/lib"

	"voxhold-backend/internal/channel"
	"voxhold-backend/internal/permissions"
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
	isNews := kind == channel.KindNews
	storageKind := kind
	if isNews {
		storageKind = channel.KindText
	}

	const query = `
	INSERT INTO channels (
		server_id,
		name,
		kind,
		is_news,
		position,
		created_by
	)
	SELECT
		?,
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
		CASE WHEN is_news=1 THEN 'news' ELSE kind END,
		position,
		created_by,
		created_at,
		access_restricted,
		comments_enabled
	`

	var createdChannel channel.Channel

	err := r.db.QueryRowContext(
		ctx,
		query,
		serverID,
		name,
		storageKind,
		isNews,
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
		&createdChannel.AccessRestricted,
		&createdChannel.CommentsEnabled,
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

	createdChannel.CanPublish = true
	return createdChannel, nil
}

func (r *Repository) ListByServerID(
	ctx context.Context,
	serverID int64,
	userID int64,
) ([]channel.Channel, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
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
		channels.id,
		channels.server_id,
		channels.name,
		CASE WHEN channels.is_news=1 THEN 'news' ELSE channels.kind END,
		channels.position,
		channels.created_by,
		channels.created_at,
		channels.access_restricted,
        channels.comments_enabled,
        (SELECT permissions FROM member_permissions WHERE server_id=channels.server_id AND user_id=?),
        ((SELECT (permissions & 2)<>0 FROM member_permissions WHERE server_id=channels.server_id AND user_id=?) AND (channels.is_news=0 OR EXISTS(SELECT 1 FROM server_members m WHERE m.server_id=channels.server_id AND m.user_id=? AND m.role IN ('owner','admin'))
          OR EXISTS(SELECT 1 FROM channel_publishers p WHERE p.channel_id=channels.id AND p.user_id=?))),
        (SELECT count(*) FROM channel_notifications n WHERE n.channel_id=channels.id AND n.user_id=? AND n.read_at IS NULL
          AND n.message_id>COALESCE((SELECT last_read_message_id FROM channel_reads r WHERE r.channel_id=channels.id AND r.user_id=n.user_id),0)),
		COALESCE((
			SELECT MAX(messages.id)
			FROM messages
			WHERE messages.channel_id = channels.id
		), 0) AS last_message_id
	FROM channels
	WHERE channels.server_id = ?
	  AND EXISTS (SELECT 1 FROM channel_accessible_members a WHERE a.channel_id=channels.id AND a.user_id=?)
	ORDER BY
		channels.position ASC,
		channels.id ASC
	`

	rows, err := tx.QueryContext(
		ctx,
		channelsQuery,
		userID, userID, userID, userID, userID,
		serverID,
		userID,
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
			&value.AccessRestricted,
			&value.CommentsEnabled,
			&value.Permissions,
			&value.CanPublish,
			&value.UnreadCount,
			&value.LastMessageID,
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

func (r *Repository) Update(
	ctx context.Context,
	serverID int64,
	channelID int64,
	userID int64,
	name string,
) (channel.Channel, error) {
	const query = `
	UPDATE channels
	SET name = ?
	WHERE id = ?
	  AND server_id = ?
	  AND EXISTS (
		SELECT 1
		FROM server_members
		WHERE server_members.server_id = ?
		  AND server_members.user_id = ?
		  AND server_members.role IN (?, ?)
	)
	RETURNING
		id,
		server_id,
		name,
		CASE WHEN is_news=1 THEN 'news' ELSE kind END,
		position,
		created_by,
		created_at,
		access_restricted,
		comments_enabled
	`

	var updatedChannel channel.Channel

	err := r.db.QueryRowContext(
		ctx,
		query,
		name,
		channelID,
		serverID,
		serverID,
		userID,
		server.RoleOwner,
		server.RoleAdmin,
	).Scan(
		&updatedChannel.ID,
		&updatedChannel.ServerID,
		&updatedChannel.Name,
		&updatedChannel.Kind,
		&updatedChannel.Position,
		&updatedChannel.CreatedBy,
		&updatedChannel.CreatedAt,
		&updatedChannel.AccessRestricted,
		&updatedChannel.CommentsEnabled,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			const existsQuery = `
			SELECT EXISTS (
				SELECT 1
				FROM channels
				WHERE id = ?
				  AND server_id = ?
			)
			`

			var exists bool

			if err := r.db.QueryRowContext(
				ctx,
				existsQuery,
				channelID,
				serverID,
			).Scan(&exists); err != nil {
				return channel.Channel{}, fmt.Errorf(
					"check channel existence: %w",
					err,
				)
			}

			if !exists {
				return channel.Channel{}, channel.ErrNotFound
			}

			return channel.Channel{}, channel.ErrForbidden
		}

		var sqliteError *sqlite.Error
		if errors.As(err, &sqliteError) &&
			sqliteError.Code() == sqliteLib.SQLITE_CONSTRAINT_UNIQUE {

			return channel.Channel{},
				channel.ErrNameAlreadyExists
		}

		return channel.Channel{}, fmt.Errorf(
			"update channel: %w",
			err,
		)
	}

	return updatedChannel, nil
}

func (r *Repository) Delete(
	ctx context.Context,
	serverID int64,
	channelID int64,
	userID int64,
) (channel.Channel, error) {
	const query = `
	DELETE FROM channels
	WHERE id = ?
	  AND server_id = ?
	  AND EXISTS (
		SELECT 1
		FROM server_members
		WHERE server_members.server_id = ?
		  AND server_members.user_id = ?
		  AND server_members.role IN (?, ?)
	)
	RETURNING
		id,
		server_id,
		name,
		CASE WHEN is_news=1 THEN 'news' ELSE kind END,
		position,
		created_by,
		created_at,
		access_restricted,
		comments_enabled
	`

	var deletedChannel channel.Channel

	err := r.db.QueryRowContext(
		ctx,
		query,
		channelID,
		serverID,
		serverID,
		userID,
		server.RoleOwner,
		server.RoleAdmin,
	).Scan(
		&deletedChannel.ID,
		&deletedChannel.ServerID,
		&deletedChannel.Name,
		&deletedChannel.Kind,
		&deletedChannel.Position,
		&deletedChannel.CreatedBy,
		&deletedChannel.CreatedAt,
		&deletedChannel.AccessRestricted,
		&deletedChannel.CommentsEnabled,
	)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return channel.Channel{}, fmt.Errorf(
				"delete channel: %w",
				err,
			)
		}
	} else {
		return deletedChannel, nil
	}

	const existsQuery = `
	SELECT EXISTS (
		SELECT 1
		FROM channels
		WHERE id = ?
		  AND server_id = ?
	)
	`

	var exists bool

	if err := r.db.QueryRowContext(
		ctx,
		existsQuery,
		channelID,
		serverID,
	).Scan(&exists); err != nil {
		return channel.Channel{}, fmt.Errorf(
			"check channel existence: %w",
			err,
		)
	}

	if !exists {
		return channel.Channel{}, channel.ErrNotFound
	}

	return channel.Channel{}, channel.ErrForbidden
}

func (r *Repository) CheckAccess(
	ctx context.Context,
	serverID int64,
	channelID int64,
	userID int64,
) error {
	return checkAccess(ctx, r.db, serverID, channelID, userID)
}

type accessQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func checkAccess(ctx context.Context, db accessQueryer, serverID, channelID, userID int64) error {
	const query = `
	SELECT
		EXISTS (
			SELECT 1
			FROM channels
			WHERE id = ?
			  AND server_id = ?
		),
		EXISTS (
			SELECT 1
			FROM channel_accessible_members
			WHERE channel_id = ?
			  AND user_id = ?
		)
	`

	var channelExists bool
	var userIsMember bool

	if err := db.QueryRowContext(
		ctx,
		query,
		channelID,
		serverID,
		channelID,
		userID,
	).Scan(
		&channelExists,
		&userIsMember,
	); err != nil {
		return fmt.Errorf(
			"query channel access: %w",
			err,
		)
	}

	if !channelExists {
		return channel.ErrNotFound
	}

	if !userIsMember {
		return channel.ErrForbidden
	}

	return nil
}

func (r *Repository) CheckVoiceAccess(
	ctx context.Context,
	serverID int64,
	channelID int64,
	userID int64,
) error {
	const query = `
	SELECT
		CASE WHEN channels.is_news=1 THEN 'news' ELSE channels.kind END,
		EXISTS (
			SELECT 1
			FROM channel_accessible_members a
			WHERE a.channel_id = channels.id
			  AND a.user_id = ?
		)
	FROM channels
	WHERE channels.id = ?
	  AND channels.server_id = ?
	`

	var kind channel.Kind
	var userIsMember bool

	err := r.db.QueryRowContext(
		ctx,
		query,
		userID,
		channelID,
		serverID,
	).Scan(
		&kind,
		&userIsMember,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return channel.ErrNotFound
		}

		return fmt.Errorf(
			"query voice channel access: %w",
			err,
		)
	}

	if !userIsMember {
		return channel.ErrForbidden
	}

	if kind != channel.KindVoice {
		return channel.ErrVoiceRequired
	}
	allowed, err := permissions.Allowed(ctx, r.db, serverID, userID, permissions.Voice)
	if err != nil {
		return err
	}
	if !allowed {
		return channel.ErrForbidden
	}

	return nil
}

func (r *Repository) CheckStreamAccess(ctx context.Context, serverID, channelID, userID int64) error {
	if err := r.CheckVoiceAccess(ctx, serverID, channelID, userID); err != nil {
		return err
	}
	allowed, err := permissions.Allowed(ctx, r.db, serverID, userID, permissions.Stream)
	if err != nil {
		return err
	}
	if !allowed {
		return channel.ErrForbidden
	}
	return nil
}

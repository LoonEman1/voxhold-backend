package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"voxhold-backend/internal/channel"
	"voxhold-backend/internal/readstate"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Mark(
	ctx context.Context,
	serverID int64,
	channelID int64,
	userID int64,
	lastReadMessageID int64,
) (readstate.ChannelRead, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return readstate.ChannelRead{}, false, fmt.Errorf(
			"begin mark channel read transaction: %w",
			err,
		)
	}
	defer func() { _ = tx.Rollback() }()

	if err := requireTextChannelMember(
		ctx, tx, serverID, channelID, userID,
	); err != nil {
		return readstate.ChannelRead{}, false, err
	}

	const messageQuery = `
	SELECT EXISTS (
		SELECT 1
		FROM messages
		WHERE id = ?
		  AND channel_id = ?
	)
	`

	var messageExists bool
	if err := tx.QueryRowContext(
		ctx,
		messageQuery,
		lastReadMessageID,
		channelID,
	).Scan(&messageExists); err != nil {
		return readstate.ChannelRead{}, false, fmt.Errorf(
			"check read message existence: %w",
			err,
		)
	}

	if !messageExists {
		return readstate.ChannelRead{}, false,
			readstate.ErrMessageNotFound
	}

	const upsertQuery = `
	INSERT INTO channel_reads (
		channel_id,
		user_id,
		last_read_message_id
	)
	VALUES (?, ?, ?)
	ON CONFLICT (channel_id, user_id) DO UPDATE
	SET
		last_read_message_id = excluded.last_read_message_id,
		updated_at = unixepoch()
	WHERE excluded.last_read_message_id >
		channel_reads.last_read_message_id
	RETURNING
		channel_id,
		user_id,
		last_read_message_id,
		updated_at
	`

	read := readstate.ChannelRead{ServerID: serverID}
	err = tx.QueryRowContext(
		ctx,
		upsertQuery,
		channelID,
		userID,
		lastReadMessageID,
	).Scan(
		&read.ChannelID,
		&read.UserID,
		&read.LastReadMessageID,
		&read.UpdatedAt,
	)

	changed := true
	if errors.Is(err, sql.ErrNoRows) {
		changed = false
		read, err = selectChannelRead(
			ctx, tx, serverID, channelID, userID,
		)
	}
	if err != nil {
		return readstate.ChannelRead{}, false, fmt.Errorf(
			"upsert channel read: %w",
			err,
		)
	}

	if err := tx.Commit(); err != nil {
		return readstate.ChannelRead{}, false, fmt.Errorf(
			"commit mark channel read transaction: %w",
			err,
		)
	}

	return read, changed, nil
}

func (r *Repository) ListByUserID(
	ctx context.Context,
	userID int64,
) ([]readstate.ChannelRead, error) {
	const query = `
	SELECT
		channels.server_id,
		channel_reads.channel_id,
		channel_reads.user_id,
		channel_reads.last_read_message_id,
		channel_reads.updated_at
	FROM channel_reads
	JOIN channels
		ON channels.id = channel_reads.channel_id
	JOIN server_members
		ON server_members.server_id = channels.server_id
		AND server_members.user_id = channel_reads.user_id
	WHERE channel_reads.user_id = ?
	ORDER BY
		channels.server_id ASC,
		channel_reads.channel_id ASC
	`

	return queryChannelReads(ctx, r.db, query, userID)
}

func (r *Repository) ListByChannelID(
	ctx context.Context,
	serverID int64,
	channelID int64,
	requesterUserID int64,
) ([]readstate.ChannelRead, error) {
	tx, err := r.db.BeginTx(
		ctx,
		&sql.TxOptions{ReadOnly: true},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"begin list channel reads transaction: %w",
			err,
		)
	}
	defer func() { _ = tx.Rollback() }()

	if err := requireTextChannelMember(
		ctx,
		tx,
		serverID,
		channelID,
		requesterUserID,
	); err != nil {
		return nil, err
	}

	const query = `
	SELECT
		?,
		channel_reads.channel_id,
		channel_reads.user_id,
		channel_reads.last_read_message_id,
		channel_reads.updated_at
	FROM channel_reads
	JOIN server_members
		ON server_members.server_id = ?
		AND server_members.user_id = channel_reads.user_id
	WHERE channel_reads.channel_id = ?
	ORDER BY channel_reads.user_id ASC
	`

	reads, err := queryChannelReads(
		ctx, tx, query, serverID, serverID, channelID,
	)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf(
			"commit list channel reads transaction: %w",
			err,
		)
	}

	return reads, nil
}

type queryer interface {
	QueryContext(
		ctx context.Context,
		query string,
		args ...any,
	) (*sql.Rows, error)
}

func queryChannelReads(
	ctx context.Context,
	db queryer,
	query string,
	args ...any,
) ([]readstate.ChannelRead, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf(
			"query channel reads: %w",
			err,
		)
	}
	defer rows.Close()

	reads := make([]readstate.ChannelRead, 0)
	for rows.Next() {
		var read readstate.ChannelRead

		if err := rows.Scan(
			&read.ServerID,
			&read.ChannelID,
			&read.UserID,
			&read.LastReadMessageID,
			&read.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf(
				"scan channel read: %w",
				err,
			)
		}

		reads = append(reads, read)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf(
			"iterate channel reads: %w",
			err,
		)
	}

	return reads, nil
}

func selectChannelRead(
	ctx context.Context,
	tx *sql.Tx,
	serverID int64,
	channelID int64,
	userID int64,
) (readstate.ChannelRead, error) {
	const query = `
	SELECT
		channel_id,
		user_id,
		last_read_message_id,
		updated_at
	FROM channel_reads
	WHERE channel_id = ?
	  AND user_id = ?
	`

	read := readstate.ChannelRead{ServerID: serverID}
	if err := tx.QueryRowContext(
		ctx, query, channelID, userID,
	).Scan(
		&read.ChannelID,
		&read.UserID,
		&read.LastReadMessageID,
		&read.UpdatedAt,
	); err != nil {
		return readstate.ChannelRead{}, fmt.Errorf(
			"select channel read: %w",
			err,
		)
	}

	return read, nil
}

func requireTextChannelMember(
	ctx context.Context,
	tx *sql.Tx,
	serverID int64,
	channelID int64,
	userID int64,
) error {
	const query = `
	SELECT
		channels.kind,
		EXISTS (
			SELECT 1
			FROM server_members
			WHERE server_members.server_id = ?
			  AND server_members.user_id = ?
		)
	FROM channels
	WHERE channels.id = ?
	  AND channels.server_id = ?
	`

	var kind channel.Kind
	var isMember bool

	if err := tx.QueryRowContext(
		ctx,
		query,
		serverID,
		userID,
		channelID,
		serverID,
	).Scan(&kind, &isMember); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return readstate.ErrChannelNotFound
		}

		return fmt.Errorf(
			"check channel read access: %w",
			err,
		)
	}

	if !isMember {
		return readstate.ErrForbidden
	}

	if kind != channel.KindText {
		return readstate.ErrTextChannelRequired
	}

	return nil
}

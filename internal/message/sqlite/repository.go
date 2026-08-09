package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"voxhold-backend/internal/channel"
	"voxhold-backend/internal/message"
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
	channelID int64,
	authorUserID int64,
	content string,
) (message.Message, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return message.Message{}, fmt.Errorf(
			"begin create message transaction: %w",
			err,
		)
	}

	defer func() {
		_ = tx.Rollback()
	}()

	username, err := requireServerMember(
		ctx,
		tx,
		serverID,
		authorUserID,
	)
	if err != nil {
		return message.Message{}, err
	}

	if err := requireTextChannel(
		ctx,
		tx,
		serverID,
		channelID,
	); err != nil {
		return message.Message{}, err
	}

	const query = `
	INSERT INTO messages (
		channel_id,
		author_user_id,
		content
	)
	VALUES (?, ?, ?)
	RETURNING
		id,
		channel_id,
		content,
		created_at,
		edited_at
	`

	createdMessage := message.Message{
		Author: message.Author{
			UserID:   authorUserID,
			Username: username,
		},
	}

	err = tx.QueryRowContext(
		ctx,
		query,
		channelID,
		authorUserID,
		content,
	).Scan(
		&createdMessage.ID,
		&createdMessage.ChannelID,
		&createdMessage.Content,
		&createdMessage.CreatedAt,
		&createdMessage.EditedAt,
	)
	if err != nil {
		return message.Message{}, fmt.Errorf(
			"insert message: %w",
			err,
		)
	}

	if err := tx.Commit(); err != nil {
		return message.Message{}, fmt.Errorf(
			"commit create message transaction: %w",
			err,
		)
	}

	return createdMessage, nil
}

func (r *Repository) ListByChannelID(
	ctx context.Context,
	serverID int64,
	channelID int64,
	userID int64,
	beforeID *int64,
	limit int,
) ([]message.Message, error) {
	tx, err := r.db.BeginTx(
		ctx,
		&sql.TxOptions{
			ReadOnly: true,
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"begin list messages transaction: %w",
			err,
		)
	}

	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := requireServerMember(
		ctx,
		tx,
		serverID,
		userID,
	); err != nil {
		return nil, err
	}

	if err := requireTextChannel(
		ctx,
		tx,
		serverID,
		channelID,
	); err != nil {
		return nil, err
	}

	const firstPageCursor int64 = 1<<63 - 1
	cursor := firstPageCursor

	if beforeID != nil {
		cursor = *beforeID
	}

	const query = `
	SELECT
		messages.id,
		messages.channel_id,
		messages.author_user_id,
		users.username,
		messages.content,
		messages.created_at,
		messages.edited_at
	FROM messages
	JOIN users
		ON users.id = messages.author_user_id
	WHERE messages.channel_id = ?
	  AND messages.id < ?
	ORDER BY messages.id DESC
	LIMIT ?
	`

	rows, err := tx.QueryContext(
		ctx,
		query,
		channelID,
		cursor,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"query channel messages: %w",
			err,
		)
	}
	defer rows.Close()

	messages := make(
		[]message.Message,
		0,
		limit,
	)

	for rows.Next() {
		var value message.Message

		if err := rows.Scan(
			&value.ID,
			&value.ChannelID,
			&value.Author.UserID,
			&value.Author.Username,
			&value.Content,
			&value.CreatedAt,
			&value.EditedAt,
		); err != nil {
			return nil, fmt.Errorf(
				"scan channel message: %w",
				err,
			)
		}

		messages = append(messages, value)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf(
			"iterate channel messages: %w",
			err,
		)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf(
			"commit list messages transaction: %w",
			err,
		)
	}

	return messages, nil
}

func requireServerMember(
	ctx context.Context,
	tx *sql.Tx,
	serverID int64,
	userID int64,
) (string, error) {
	const query = `
	SELECT users.username
	FROM server_members
	JOIN users
		ON users.id = server_members.user_id
	WHERE server_members.server_id = ?
	  AND server_members.user_id = ?
	`

	var username string

	err := tx.QueryRowContext(
		ctx,
		query,
		serverID,
		userID,
	).Scan(&username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", message.ErrForbidden
		}

		return "", fmt.Errorf(
			"check message server membership: %w",
			err,
		)
	}

	return username, nil
}

func requireTextChannel(
	ctx context.Context,
	tx *sql.Tx,
	serverID int64,
	channelID int64,
) error {
	const query = `
	SELECT kind
	FROM channels
	WHERE id = ?
	  AND server_id = ?
	`

	var kind channel.Kind

	err := tx.QueryRowContext(
		ctx,
		query,
		channelID,
		serverID,
	).Scan(&kind)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return message.ErrChannelNotFound
		}

		return fmt.Errorf(
			"find message channel: %w",
			err,
		)
	}

	if kind != channel.KindText {
		return message.ErrTextChannelRequired
	}

	return nil
}

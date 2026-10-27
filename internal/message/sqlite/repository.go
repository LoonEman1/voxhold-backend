package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"voxhold-backend/internal/channel"
	"voxhold-backend/internal/message"
	serverDomain "voxhold-backend/internal/server"
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
	return r.CreateIdempotent(ctx, serverID, channelID, authorUserID, content, "")
}

func (r *Repository) CreateIdempotent(ctx context.Context, serverID, channelID, authorUserID int64, content, clientMessageID string) (message.Message, error) {
	return r.CreateReferenced(ctx, serverID, channelID, authorUserID, message.CreateInput{Content: content, ClientMessageID: clientMessageID}, nil)
}

func (r *Repository) CreateReferenced(ctx context.Context, serverID, channelID, authorUserID int64, input message.CreateInput, forward *message.ForwardInput) (message.Message, error) {
	content, clientMessageID := input.Content, input.ClientMessageID
	kind, contentHash := requestHash(input, forward)
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

	if err := requireMessageChannel(
		ctx,
		tx,
		serverID,
		channelID,
		authorUserID,
	); err != nil {
		return message.Message{}, err
	}
	if clientMessageID != "" {
		var original, originalKind string
		var existingID sql.NullInt64
		err := tx.QueryRowContext(ctx, `SELECT content_hash, message_id, request_kind FROM message_send_keys
			WHERE channel_id = ? AND author_user_id = ? AND client_message_id = ?`, channelID, authorUserID, clientMessageID).Scan(&original, &existingID, &originalKind)
		if err == nil {
			if original != contentHash || originalKind != kind {
				return message.Message{}, message.ErrIdempotencyConflict
			}
			if !existingID.Valid {
				return message.Message{}, message.ErrMessageNotFound
			}
			value, err := findMessage(ctx, tx, channelID, existingID.Int64)
			if err != nil {
				return value, err
			}
			if err = hydrateInteractions(ctx, tx, authorUserID, []*message.Message{&value}); err != nil {
				return value, err
			}
			value.ClientMessageID = clientMessageID
			value.Replayed = true
			return value, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return message.Message{}, fmt.Errorf("find send key: %w", err)
		}
	}

	var reply *message.ReplyReference
	var replyID any
	if input.ReplyToMessageID != nil {
		target, err := findMessage(ctx, tx, channelID, *input.ReplyToMessageID)
		if errors.Is(err, message.ErrMessageNotFound) {
			return message.Message{}, message.ErrReplyInvalid
		}
		if err != nil {
			return message.Message{}, err
		}
		replyID = target.ID
		reply = &message.ReplyReference{MessageID: target.ID, Author: target.Author, Content: target.Content, Revision: target.Revision}
	}
	var forwarded *message.ForwardReference
	var forwardData any
	if forward != nil {
		content, forwarded, err = forwardSnapshot(ctx, tx, authorUserID, *forward)
		if err != nil {
			return message.Message{}, err
		}
		encoded, err := json.Marshal(forwarded)
		if err != nil {
			return message.Message{}, err
		}
		forwardData = string(encoded)
	}
	const query = `
	INSERT INTO messages (
		channel_id,
		author_user_id,
		content,
        reply_to_message_id,
        reply_to_original_id,
        forward_data
	)
	VALUES (?, ?, ?, ?, ?, ?)
	RETURNING
		id,
		channel_id,
		content,
		created_at,
		edited_at,
		revision
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
		replyID, replyID, forwardData,
	).Scan(
		&createdMessage.ID,
		&createdMessage.ChannelID,
		&createdMessage.Content,
		&createdMessage.CreatedAt,
		&createdMessage.EditedAt,
		&createdMessage.Revision,
	)
	if err != nil {
		return message.Message{}, fmt.Errorf(
			"insert message: %w",
			err,
		)
	}

	createdMessage.Reply = reply
	createdMessage.Forward = forwarded
	createdMessage.Reactions = []message.Reaction{}
	createdMessage.ReactionsRevision = createdMessage.Revision
	if clientMessageID != "" {
		if _, err := tx.ExecContext(ctx, `INSERT INTO message_send_keys(channel_id, author_user_id, client_message_id, content_hash, message_id, request_kind)
			VALUES(?, ?, ?, ?, ?, ?)`, channelID, authorUserID, clientMessageID, contentHash, createdMessage.ID, kind); err != nil {
			return message.Message{}, fmt.Errorf("store send key: %w", err)
		}
		createdMessage.ClientMessageID = clientMessageID
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

	if err := requireMessageChannel(
		ctx,
		tx,
		serverID,
		channelID,
		userID,
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
		messages.edited_at,
		messages.revision
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
			&value.Revision,
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

	if err := hydrateInteractions(ctx, tx, userID, messagePointers(messages)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf(
			"commit list messages transaction: %w",
			err,
		)
	}

	return messages, nil
}

func (r *Repository) Update(
	ctx context.Context,
	serverID int64,
	channelID int64,
	messageID int64,
	userID int64,
	content string,
) (message.Message, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return message.Message{}, fmt.Errorf(
			"begin update message transaction: %w",
			err,
		)
	}
	defer func() { _ = tx.Rollback() }()

	username, err := requireServerMember(
		ctx, tx, serverID, userID,
	)
	if err != nil {
		return message.Message{}, err
	}

	if err := requireMessageChannel(
		ctx, tx, serverID, channelID, userID,
	); err != nil {
		return message.Message{}, err
	}

	var forwardedData sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT forward_data FROM messages WHERE id=? AND channel_id=?`, messageID, channelID).Scan(&forwardedData); err == nil && forwardedData.Valid {
		return message.Message{}, message.ErrForwardEdit
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return message.Message{}, err
	}
	const query = `
	UPDATE messages
	SET
		content = ?,
		edited_at = unixepoch(),
		revision = revision + 1
	WHERE id = ?
	  AND channel_id = ?
	  AND author_user_id = ?
	RETURNING
		id,
		channel_id,
		content,
		created_at,
		edited_at,
		revision
	`

	updatedMessage := message.Message{
		Author: message.Author{
			UserID:   userID,
			Username: username,
		},
	}

	err = tx.QueryRowContext(
		ctx,
		query,
		content,
		messageID,
		channelID,
		userID,
	).Scan(
		&updatedMessage.ID,
		&updatedMessage.ChannelID,
		&updatedMessage.Content,
		&updatedMessage.CreatedAt,
		&updatedMessage.EditedAt,
		&updatedMessage.Revision,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if _, findErr := findMessage(
				ctx, tx, channelID, messageID,
			); findErr != nil {
				return message.Message{}, findErr
			}

			return message.Message{}, message.ErrEditForbidden
		}

		return message.Message{}, fmt.Errorf(
			"update message row: %w",
			err,
		)
	}

	if err := hydrateInteractions(ctx, tx, userID, []*message.Message{&updatedMessage}); err != nil {
		return message.Message{}, err
	}
	if err := tx.Commit(); err != nil {
		return message.Message{}, fmt.Errorf(
			"commit update message transaction: %w",
			err,
		)
	}

	return updatedMessage, nil
}

func (r *Repository) Delete(
	ctx context.Context,
	serverID int64,
	channelID int64,
	messageID int64,
	userID int64,
) (message.Message, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return message.Message{}, fmt.Errorf(
			"begin delete message transaction: %w",
			err,
		)
	}
	defer func() { _ = tx.Rollback() }()

	_, role, err := requireServerMemberWithRole(
		ctx, tx, serverID, userID,
	)
	if err != nil {
		return message.Message{}, err
	}

	if err := requireMessageChannel(
		ctx, tx, serverID, channelID, userID,
	); err != nil {
		return message.Message{}, err
	}

	deletedMessage, err := findMessage(
		ctx, tx, channelID, messageID,
	)
	if err != nil {
		return message.Message{}, err
	}

	canModerate := role == serverDomain.RoleOwner ||
		role == serverDomain.RoleAdmin

	if deletedMessage.Author.UserID != userID && !canModerate {
		return message.Message{}, message.ErrDeleteForbidden
	}

	result, err := tx.ExecContext(
		ctx,
		`DELETE FROM messages WHERE id = ?`,
		messageID,
	)
	if err != nil {
		return message.Message{}, fmt.Errorf(
			"delete message row: %w",
			err,
		)
	}

	if affected, err := result.RowsAffected(); err != nil {
		return message.Message{}, fmt.Errorf(
			"count deleted messages: %w",
			err,
		)
	} else if affected != 1 {
		return message.Message{}, message.ErrMessageNotFound
	}

	if err := tx.Commit(); err != nil {
		return message.Message{}, fmt.Errorf(
			"commit delete message transaction: %w",
			err,
		)
	}

	return deletedMessage, nil
}

func (r *Repository) Pin(
	ctx context.Context,
	serverID int64,
	channelID int64,
	messageID int64,
	userID int64,
) (message.PinnedMessage, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return message.PinnedMessage{}, false, fmt.Errorf(
			"begin pin message transaction: %w",
			err,
		)
	}
	defer func() { _ = tx.Rollback() }()

	_, role, err := requireServerMemberWithRole(
		ctx, tx, serverID, userID,
	)
	if err != nil {
		return message.PinnedMessage{}, false, err
	}

	if role != serverDomain.RoleOwner &&
		role != serverDomain.RoleAdmin {
		return message.PinnedMessage{}, false, message.ErrPinForbidden
	}

	if err := requireMessageChannel(
		ctx, tx, serverID, channelID, userID,
	); err != nil {
		return message.PinnedMessage{}, false, err
	}

	value, err := findMessage(
		ctx, tx, channelID, messageID,
	)
	if err != nil {
		return message.PinnedMessage{}, false, err
	}

	const insertQuery = `
	INSERT INTO message_pins (
		message_id,
		pinned_by_user_id
	)
	VALUES (?, ?)
	ON CONFLICT(message_id) DO NOTHING
	`

	result, err := tx.ExecContext(
		ctx, insertQuery, messageID, userID,
	)
	if err != nil {
		return message.PinnedMessage{}, false, fmt.Errorf(
			"insert message pin: %w",
			err,
		)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return message.PinnedMessage{}, false, fmt.Errorf(
			"count inserted message pins: %w",
			err,
		)
	}
	created := affected == 1

	pinnedMessage := message.PinnedMessage{Message: value}
	const selectQuery = `
	SELECT
		message_pins.pinned_by_user_id,
		users.username,
		message_pins.pinned_at
	FROM message_pins
	JOIN users
		ON users.id = message_pins.pinned_by_user_id
	WHERE message_pins.message_id = ?
	`

	err = tx.QueryRowContext(
		ctx, selectQuery, messageID,
	).Scan(
		&pinnedMessage.PinnedBy.UserID,
		&pinnedMessage.PinnedBy.Username,
		&pinnedMessage.PinnedAt,
	)
	if err != nil {
		return message.PinnedMessage{}, false, fmt.Errorf(
			"select message pin: %w",
			err,
		)
	}

	if err := hydrateInteractions(ctx, tx, userID, []*message.Message{&pinnedMessage.Message}); err != nil {
		return message.PinnedMessage{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return message.PinnedMessage{}, false, fmt.Errorf(
			"commit pin message transaction: %w",
			err,
		)
	}

	return pinnedMessage, created, nil
}

func (r *Repository) Unpin(
	ctx context.Context,
	serverID int64,
	channelID int64,
	messageID int64,
	userID int64,
) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf(
			"begin unpin message transaction: %w",
			err,
		)
	}
	defer func() { _ = tx.Rollback() }()

	_, role, err := requireServerMemberWithRole(
		ctx, tx, serverID, userID,
	)
	if err != nil {
		return false, err
	}

	if role != serverDomain.RoleOwner &&
		role != serverDomain.RoleAdmin {
		return false, message.ErrPinForbidden
	}

	if err := requireMessageChannel(
		ctx, tx, serverID, channelID, userID,
	); err != nil {
		return false, err
	}

	if _, err := findMessage(
		ctx, tx, channelID, messageID,
	); err != nil {
		return false, err
	}

	result, err := tx.ExecContext(
		ctx,
		`DELETE FROM message_pins WHERE message_id = ?`,
		messageID,
	)
	if err != nil {
		return false, fmt.Errorf(
			"delete message pin: %w",
			err,
		)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf(
			"count deleted message pins: %w",
			err,
		)
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf(
			"commit unpin message transaction: %w",
			err,
		)
	}

	return affected == 1, nil
}

func (r *Repository) ListPinned(
	ctx context.Context,
	serverID int64,
	channelID int64,
	userID int64,
) ([]message.PinnedMessage, error) {
	tx, err := r.db.BeginTx(
		ctx,
		&sql.TxOptions{ReadOnly: true},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"begin list pinned messages transaction: %w",
			err,
		)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := requireServerMember(
		ctx, tx, serverID, userID,
	); err != nil {
		return nil, err
	}

	if err := requireMessageChannel(
		ctx, tx, serverID, channelID, userID,
	); err != nil {
		return nil, err
	}

	const query = `
	SELECT
		messages.id,
		messages.channel_id,
		messages.author_user_id,
		authors.username,
		messages.content,
		messages.created_at,
		messages.edited_at,
		messages.revision,
		message_pins.pinned_by_user_id,
		pinners.username,
		message_pins.pinned_at
	FROM message_pins
	JOIN messages
		ON messages.id = message_pins.message_id
	JOIN users AS authors
		ON authors.id = messages.author_user_id
	JOIN users AS pinners
		ON pinners.id = message_pins.pinned_by_user_id
	WHERE messages.channel_id = ?
	ORDER BY
		message_pins.pinned_at DESC,
		messages.id DESC
	`

	rows, err := tx.QueryContext(ctx, query, channelID)
	if err != nil {
		return nil, fmt.Errorf(
			"query pinned messages: %w",
			err,
		)
	}
	defer rows.Close()

	values := make([]message.PinnedMessage, 0)

	for rows.Next() {
		var value message.PinnedMessage

		if err := rows.Scan(
			&value.Message.ID,
			&value.Message.ChannelID,
			&value.Message.Author.UserID,
			&value.Message.Author.Username,
			&value.Message.Content,
			&value.Message.CreatedAt,
			&value.Message.EditedAt,
			&value.Message.Revision,
			&value.PinnedBy.UserID,
			&value.PinnedBy.Username,
			&value.PinnedAt,
		); err != nil {
			return nil, fmt.Errorf(
				"scan pinned message: %w",
				err,
			)
		}

		values = append(values, value)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf(
			"iterate pinned messages: %w",
			err,
		)
	}

	if err := hydrateInteractions(ctx, tx, userID, pinPointers(values)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf(
			"commit list pinned messages transaction: %w",
			err,
		)
	}

	return values, nil
}

func findMessage(
	ctx context.Context,
	tx *sql.Tx,
	channelID int64,
	messageID int64,
) (message.Message, error) {
	const query = `
	SELECT
		messages.id,
		messages.channel_id,
		messages.author_user_id,
		users.username,
		messages.content,
		messages.created_at,
		messages.edited_at,
		messages.revision
	FROM messages
	JOIN users
		ON users.id = messages.author_user_id
	WHERE messages.id = ?
	  AND messages.channel_id = ?
	`

	var value message.Message

	err := tx.QueryRowContext(
		ctx, query, messageID, channelID,
	).Scan(
		&value.ID,
		&value.ChannelID,
		&value.Author.UserID,
		&value.Author.Username,
		&value.Content,
		&value.CreatedAt,
		&value.EditedAt,
		&value.Revision,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return message.Message{}, message.ErrMessageNotFound
		}

		return message.Message{}, fmt.Errorf(
			"find message: %w",
			err,
		)
	}

	return value, nil
}

func requireServerMember(
	ctx context.Context,
	tx *sql.Tx,
	serverID int64,
	userID int64,
) (string, error) {
	username, _, err := requireServerMemberWithRole(
		ctx,
		tx,
		serverID,
		userID,
	)

	return username, err
}

func requireServerMemberWithRole(
	ctx context.Context,
	tx *sql.Tx,
	serverID int64,
	userID int64,
) (string, serverDomain.Role, error) {
	const query = `
	SELECT
		users.username,
		server_members.role
	FROM server_members
	JOIN users
		ON users.id = server_members.user_id
	WHERE server_members.server_id = ?
	  AND server_members.user_id = ?
	`

	var username string
	var role serverDomain.Role

	err := tx.QueryRowContext(
		ctx,
		query,
		serverID,
		userID,
	).Scan(
		&username,
		&role,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", message.ErrForbidden
		}

		return "", "", fmt.Errorf(
			"check message server membership: %w",
			err,
		)
	}

	return username, role, nil
}

func requireMessageChannel(
	ctx context.Context,
	tx *sql.Tx,
	serverID int64,
	channelID int64,
	userID int64,
) error {
	const query = `
	SELECT kind, EXISTS(SELECT 1 FROM channel_accessible_members a WHERE a.channel_id=channels.id AND a.user_id=?)
	FROM channels
	WHERE id = ?
	  AND server_id = ?
	`

	var kind channel.Kind
	var allowed bool

	err := tx.QueryRowContext(
		ctx,
		query,
		userID,
		channelID,
		serverID,
	).Scan(&kind, &allowed)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return message.ErrChannelNotFound
		}

		return fmt.Errorf(
			"find message channel: %w",
			err,
		)
	}

	if !allowed {
		return message.ErrForbidden
	}
	if kind != channel.KindText && kind != channel.KindVoice {
		return message.ErrTextChannelRequired
	}

	return nil
}

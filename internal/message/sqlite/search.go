package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"voxhold-backend/internal/message"
)

func (r *Repository) Search(
	ctx context.Context,
	serverID int64,
	userID int64,
	query string,
	beforeID *int64,
	limit int,
) ([]message.SearchResult, error) {
	tx, err := r.db.BeginTx(
		ctx,
		&sql.TxOptions{ReadOnly: true},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"begin search messages transaction: %w",
			err,
		)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := requireServerMember(
		ctx, tx, serverID, userID,
	); err != nil {
		return nil, err
	}

	const firstPageCursor int64 = 1<<63 - 1
	cursor := firstPageCursor
	if beforeID != nil {
		cursor = *beforeID
	}

	const searchQuery = `
	SELECT
		messages.id,
		messages.channel_id,
		channels.name,
		messages.author_user_id,
		users.username,
		messages.content,
		messages.created_at,
		messages.edited_at
	FROM message_search
	JOIN messages
		ON messages.id = message_search.rowid
	JOIN channels
		ON channels.id = messages.channel_id
	JOIN users
		ON users.id = messages.author_user_id
	WHERE message_search MATCH ?
	  AND channels.server_id = ?
	  AND channels.kind = 'text'
	  AND messages.id < ?
	ORDER BY messages.id DESC
	LIMIT ?
	`

	rows, err := tx.QueryContext(
		ctx,
		searchQuery,
		ftsPrefixQuery(query),
		serverID,
		cursor,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"query message search index: %w",
			err,
		)
	}
	defer rows.Close()

	results := make(
		[]message.SearchResult,
		0,
		limit,
	)

	for rows.Next() {
		var result message.SearchResult

		if err := rows.Scan(
			&result.Message.ID,
			&result.Message.ChannelID,
			&result.ChannelName,
			&result.Message.Author.UserID,
			&result.Message.Author.Username,
			&result.Message.Content,
			&result.Message.CreatedAt,
			&result.Message.EditedAt,
		); err != nil {
			return nil, fmt.Errorf(
				"scan message search result: %w",
				err,
			)
		}

		results = append(results, result)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf(
			"iterate message search results: %w",
			err,
		)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf(
			"commit search messages transaction: %w",
			err,
		)
	}

	return results, nil
}

func (r *Repository) GetContext(
	ctx context.Context,
	serverID int64,
	channelID int64,
	messageID int64,
	userID int64,
	before int,
	after int,
) (message.Context, error) {
	tx, err := r.db.BeginTx(
		ctx,
		&sql.TxOptions{ReadOnly: true},
	)
	if err != nil {
		return message.Context{}, fmt.Errorf(
			"begin message context transaction: %w",
			err,
		)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := requireServerMember(
		ctx, tx, serverID, userID,
	); err != nil {
		return message.Context{}, err
	}

	if err := requireTextChannel(
		ctx, tx, serverID, channelID,
	); err != nil {
		return message.Context{}, err
	}

	target, err := findMessage(
		ctx, tx, channelID, messageID,
	)
	if err != nil {
		return message.Context{}, err
	}

	older, err := queryMessageContextSide(
		ctx,
		tx,
		channelID,
		messageID,
		before,
		true,
	)
	if err != nil {
		return message.Context{}, err
	}
	slices.Reverse(older)

	newer, err := queryMessageContextSide(
		ctx,
		tx,
		channelID,
		messageID,
		after,
		false,
	)
	if err != nil {
		return message.Context{}, err
	}

	messages := make(
		[]message.Message,
		0,
		len(older)+1+len(newer),
	)
	messages = append(messages, older...)
	targetIndex := len(messages)
	messages = append(messages, target)
	messages = append(messages, newer...)

	if err := tx.Commit(); err != nil {
		return message.Context{}, fmt.Errorf(
			"commit message context transaction: %w",
			err,
		)
	}

	return message.Context{
		Messages:        messages,
		TargetMessageID: messageID,
		TargetIndex:     targetIndex,
	}, nil
}

func ftsPrefixQuery(query string) string {
	tokens := strings.FieldsFunc(
		query,
		func(value rune) bool {
			return !unicode.IsLetter(value) &&
				!unicode.IsNumber(value)
		},
	)

	terms := make([]string, 0, len(tokens))
	for _, token := range tokens {
		escaped := strings.ReplaceAll(token, `"`, `""`)
		terms = append(terms, `"`+escaped+`"*`)
	}

	return strings.Join(terms, " AND ")
}

func queryMessageContextSide(
	ctx context.Context,
	tx *sql.Tx,
	channelID int64,
	messageID int64,
	limit int,
	older bool,
) ([]message.Message, error) {
	query := `
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
	  AND messages.id > ?
	ORDER BY messages.id ASC
	LIMIT ?
	`

	if older {
		query = `
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
	}

	rows, err := tx.QueryContext(
		ctx, query, channelID, messageID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"query message context side: %w",
			err,
		)
	}
	defer rows.Close()

	messages := make([]message.Message, 0, limit)
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
				"scan message context side: %w",
				err,
			)
		}

		messages = append(messages, value)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf(
			"iterate message context side: %w",
			err,
		)
	}

	return messages, nil
}

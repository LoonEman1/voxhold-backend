package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"voxhold-backend/internal/message"
)

// Called inside the same writer transaction as the insert. Reading, reacting
// and publishing are independent permissions; even publishers still need ACL.
func requireNewsWrite(ctx context.Context, tx *sql.Tx, channelID, userID int64, replyID *int64) (*int64, error) {
	var news, comments, publisher bool
	err := tx.QueryRowContext(ctx, `SELECT c.is_news,c.comments_enabled,
      EXISTS(SELECT 1 FROM server_members m WHERE m.server_id=c.server_id AND m.user_id=? AND m.role IN ('owner','admin'))
      OR EXISTS(SELECT 1 FROM channel_publishers p WHERE p.channel_id=c.id AND p.user_id=?)
      FROM channels c WHERE c.id=?`, userID, userID, channelID).Scan(&news, &comments, &publisher)
	if err != nil {
		return nil, err
	}
	if !news {
		return nil, nil
	}
	if replyID == nil {
		if !publisher {
			return nil, message.ErrForbidden
		}
		return nil, nil
	}
	if !comments {
		return nil, message.ErrForbidden
	}
	var rootID int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(thread_root_id,id) FROM messages WHERE id=? AND channel_id=?`, *replyID, channelID).Scan(&rootID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, message.ErrReplyInvalid
		}
		return nil, err
	}
	return &rootID, nil
}

func (r *Repository) GetPost(ctx context.Context, serverID, channelID, userID, postID int64) (message.Message, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return message.Message{}, err
	}
	defer tx.Rollback()
	if err := requireMessageChannel(ctx, tx, serverID, channelID, userID); err != nil {
		return message.Message{}, err
	}
	value, err := findMessage(ctx, tx, channelID, postID)
	if err != nil {
		return value, err
	}
	if err := hydrateInteractions(ctx, tx, userID, []*message.Message{&value}); err != nil {
		return value, err
	}
	return value, tx.Commit()
}

func (r *Repository) ListComments(ctx context.Context, serverID, channelID, userID, postID int64, beforeID *int64, limit int) ([]message.Message, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := requireMessageChannel(ctx, tx, serverID, channelID, userID); err != nil {
		return nil, err
	}
	var valid bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM messages m JOIN channels c ON c.id=m.channel_id WHERE m.id=? AND c.id=? AND c.is_news=1 AND m.thread_root_id IS NULL)`, postID, channelID).Scan(&valid); err != nil {
		return nil, err
	}
	if !valid {
		return nil, message.ErrMessageNotFound
	}
	cursor := int64(1<<63 - 1)
	if beforeID != nil {
		cursor = *beforeID
	}
	rows, err := tx.QueryContext(ctx, `SELECT m.id,m.channel_id,m.author_user_id,u.username,m.content,m.created_at,m.edited_at,m.revision
      FROM messages m JOIN users u ON u.id=m.author_user_id WHERE m.channel_id=? AND m.thread_root_id=? AND m.id<? ORDER BY m.id DESC LIMIT ?`, channelID, postID, cursor, limit)
	if err != nil {
		return nil, err
	}
	values := []message.Message{}
	for rows.Next() {
		var value message.Message
		if err = rows.Scan(&value.ID, &value.ChannelID, &value.Author.UserID, &value.Author.Username, &value.Content, &value.CreatedAt, &value.EditedAt, &value.Revision); err != nil {
			break
		}
		values = append(values, value)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	if err := hydrateInteractions(ctx, tx, userID, messagePointers(values)); err != nil {
		return nil, err
	}
	return values, tx.Commit()
}

package community

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"time"
	"voxhold-backend/internal/message"
)

type InboxItem struct {
	MessageID    int64  `json:"message_id"`
	ServerID     int64  `json:"server_id"`
	ChannelID    int64  `json:"channel_id"`
	ThreadRootID *int64 `json:"thread_root_id,omitempty"`
	ServerName   string `json:"server_name"`
	ChannelName  string `json:"channel_name"`
	Content      string `json:"content"`
	Author       string `json:"author"`
	CreatedAt    int64  `json:"created_at"`
	Reason       string `json:"reason"`
}

func (s *Service) inbox(r *http.Request, userID int64) (any, error) {
	cursor := int64(1<<63 - 1)
	if raw := r.URL.Query().Get("before"); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || v <= 0 {
			return nil, ErrInvalid
		}
		cursor = v
	}
	rows, err := s.db.QueryContext(r.Context(), `WITH candidates AS (
 SELECT n.message_id FROM channel_notifications n WHERE n.user_id=? AND n.read_at IS NULL AND n.message_id<?
 UNION SELECT m.id FROM thread_subscriptions t JOIN messages m ON m.thread_root_id=t.message_id AND m.id>t.last_read_id
 WHERE t.user_id=? AND t.following=1 AND m.id<?
 ) SELECT m.id,c.server_id,c.id,m.thread_root_id,s.name,c.name,m.content,u.username,m.created_at,
 CASE WHEN m.reply_to_message_id IS NOT NULL AND EXISTS(SELECT 1 FROM messages p WHERE p.id=m.reply_to_message_id AND p.author_user_id=?) THEN 'reply' WHEN m.thread_root_id IS NOT NULL THEN 'thread' ELSE 'mention' END
 FROM candidates k JOIN messages m ON m.id=k.message_id JOIN channels c ON c.id=m.channel_id JOIN servers s ON s.id=c.server_id JOIN users u ON u.id=m.author_user_id
 WHERE m.id<? AND m.author_user_id<>? AND EXISTS(SELECT 1 FROM channel_accessible_members a WHERE a.channel_id=c.id AND a.user_id=?)
 ORDER BY m.id DESC LIMIT 51`, userID, cursor, userID, cursor, userID, cursor, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []InboxItem{}
	for rows.Next() {
		var v InboxItem
		if err := rows.Scan(&v.MessageID, &v.ServerID, &v.ChannelID, &v.ThreadRootID, &v.ServerName, &v.ChannelName, &v.Content, &v.Author, &v.CreatedAt, &v.Reason); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	more := len(items) > 50
	if more {
		items = items[:50]
	}
	return map[string]any{"items": items, "has_more": more}, nil
}
func (s *Service) readInbox(r *http.Request, userID int64) (any, error) {
	id := path(r, "messageID")
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(r.Context(), `UPDATE channel_notifications SET read_at=unixepoch() WHERE user_id=? AND message_id=?`, userID, id); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE thread_subscriptions SET last_read_id=max(last_read_id,?) WHERE user_id=? AND message_id=(SELECT thread_root_id FROM messages WHERE id=?)`, id, userID, id); err != nil {
		return nil, err
	}
	return map[string]bool{"ok": true}, tx.Commit()
}
func checkThread(ctx context.Context, tx *sql.Tx, cid, root, uid int64) error {
	if err := accessible(ctx, tx, cid, uid); err != nil {
		return err
	}
	var valid bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM messages m JOIN channels c ON c.id=m.channel_id WHERE m.id=? AND c.id=? AND c.kind='text' AND m.thread_root_id IS NULL)`, root, cid).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return sql.ErrNoRows
	}
	return nil
}
func (s *Service) thread(r *http.Request, userID int64) (any, error) {
	tx, err := s.db.BeginTx(r.Context(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	cid, root := path(r, "channelID"), path(r, "messageID")
	if err = checkThread(r.Context(), tx, cid, root, userID); err != nil {
		return nil, err
	}
	var following bool
	var read, count int64
	err = tx.QueryRowContext(r.Context(), `SELECT following,last_read_id FROM thread_subscriptions WHERE message_id=? AND user_id=?`, root, userID).Scan(&following, &read)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if err = tx.QueryRowContext(r.Context(), `SELECT count(*) FROM messages WHERE thread_root_id=? AND id>? AND author_user_id<>?`, root, read, userID).Scan(&count); err != nil {
		return nil, err
	}
	return map[string]any{"following": following, "last_read_id": read, "unread_count": count}, nil
}
func (s *Service) setThread(r *http.Request, userID int64) (any, error) {
	var input struct {
		Following  *bool `json:"following"`
		LastReadID int64 `json:"last_read_id"`
	}
	if err := decode(r, &input); err != nil {
		return nil, err
	}
	if input.LastReadID < 0 {
		return nil, ErrInvalid
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	cid, root := path(r, "channelID"), path(r, "messageID")
	if err = checkThread(r.Context(), tx, cid, root, userID); err != nil {
		return nil, err
	}
	var latest int64
	if err = tx.QueryRowContext(r.Context(), `SELECT COALESCE(max(id),0) FROM messages WHERE thread_root_id=?`, root).Scan(&latest); err != nil {
		return nil, err
	}
	if input.LastReadID > latest {
		return nil, ErrInvalid
	}
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO thread_subscriptions(message_id,user_id,following,last_read_id,explicit) VALUES(?,?,COALESCE(?,0),?,?) ON CONFLICT(message_id,user_id) DO UPDATE SET following=CASE WHEN excluded.explicit=1 THEN excluded.following ELSE following END,explicit=max(explicit,excluded.explicit),last_read_id=max(last_read_id,excluded.last_read_id)`, root, userID, input.Following, input.LastReadID, input.Following != nil); err != nil {
		return nil, err
	}
	return input, tx.Commit()
}

// RecordThread runs before notification fan-out and never re-enables an opt-out.
func (s *Service) RecordThread(v message.Message) error {
	if v.ThreadRootID == nil || v.Replayed {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `INSERT INTO thread_subscriptions(message_id,user_id) SELECT ?,? WHERE EXISTS(SELECT 1 FROM messages WHERE id=?) UNION SELECT id,author_user_id FROM messages WHERE id=? ON CONFLICT(message_id,user_id) DO UPDATE SET following=CASE WHEN explicit=0 THEN 1 ELSE following END`, *v.ThreadRootID, v.Author.UserID, v.ID, *v.ThreadRootID)
	return err
}

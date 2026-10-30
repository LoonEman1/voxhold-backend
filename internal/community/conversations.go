package community

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"unicode/utf8"
	"voxhold-backend/internal/message"
	"voxhold-backend/internal/storage"
)

// Contacts are checked both on creation and on every send. A shared room is
// required by default, and either side's block always overrides privacy mode.
func canContact(ctx context.Context, q queryer, from, to int64) error {
	if err := activeActor(ctx, q, from); err != nil {
		return err
	}
	var yes bool
	err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users u WHERE u.id=? AND u.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM user_bans WHERE user_id=u.id)
 AND NOT EXISTS(SELECT 1 FROM user_blocks WHERE (user_id=? AND blocked_user_id=?) OR (user_id=? AND blocked_user_id=?))
 AND (COALESCE((SELECT mode FROM message_privacy WHERE user_id=u.id),'shared')='everyone' OR (COALESCE((SELECT mode FROM message_privacy WHERE user_id=u.id),'shared')='shared' AND EXISTS(SELECT 1 FROM server_members a JOIN server_members b ON b.server_id=a.server_id WHERE a.user_id=? AND b.user_id=u.id))))`, to, from, to, to, from, from).Scan(&yes)
	if err != nil {
		return err
	}
	if !yes {
		return ErrForbidden
	}
	return nil
}
func (s *Service) privacy(r *http.Request, userID int64) (any, error) {
	mode := "shared"
	if r.Method == "PUT" {
		var v struct {
			Mode string `json:"mode"`
		}
		if err := decode(r, &v); err != nil {
			return nil, err
		}
		if v.Mode != "shared" && v.Mode != "everyone" && v.Mode != "none" {
			return nil, ErrInvalid
		}
		if _, err := s.db.ExecContext(r.Context(), `INSERT INTO message_privacy(user_id,mode) VALUES(?,?) ON CONFLICT(user_id) DO UPDATE SET mode=excluded.mode`, userID, v.Mode); err != nil {
			return nil, err
		}
		mode = v.Mode
	} else {
		err := s.db.QueryRowContext(r.Context(), `SELECT mode FROM message_privacy WHERE user_id=?`, userID).Scan(&mode)
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT blocked_user_id FROM user_blocks WHERE user_id=? ORDER BY blocked_user_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return map[string]any{"mode": mode, "blocked_user_ids": ids}, rows.Err()
}
func (s *Service) block(r *http.Request, userID int64) (any, error) {
	target := path(r, "userID")
	if target <= 0 || target == userID {
		return nil, ErrInvalid
	}
	var err error
	if r.Method == "DELETE" {
		_, err = s.db.ExecContext(r.Context(), `DELETE FROM user_blocks WHERE user_id=? AND blocked_user_id=?`, userID, target)
	} else {
		var found int64
		err = s.db.QueryRowContext(r.Context(), `SELECT id FROM users WHERE id=?`, target).Scan(&found)
		if err != nil {
			return nil, err
		}
		_, err = s.db.ExecContext(r.Context(), `INSERT INTO user_blocks(user_id,blocked_user_id) VALUES(?,?) ON CONFLICT DO NOTHING`, userID, target)
	}
	return map[string]bool{"ok": true}, err
}

type Conversation struct {
	ID      int64    `json:"id"`
	Name    string   `json:"name"`
	Direct  bool     `json:"direct"`
	Members []Person `json:"members"`
	Unread  int64    `json:"unread_count"`
	Muted   bool     `json:"muted"`
	Saved   bool     `json:"saved"`
}
type Person struct {
	ID       int64  `json:"user_id"`
	Username string `json:"username"`
}
type DirectMessage struct {
	Poll              *message.Poll             `json:"poll,omitempty"`
	Forward           *message.ForwardReference `json:"forward,omitempty"`
	Note              string                    `json:"note,omitempty"`
	Reactions         []message.Reaction        `json:"reactions"`
	Revision          int64                     `json:"revision"`
	ReactionsRevision int64                     `json:"reactions_revision"`
	Attachments       []message.Attachment      `json:"attachments,omitempty"`
	ID                int64                     `json:"id"`
	ConversationID    int64                     `json:"conversation_id"`
	Author            Person                    `json:"author"`
	Content           string                    `json:"content"`
	ClientMessageID   string                    `json:"client_message_id"`
	CreatedAt         int64                     `json:"created_at"`
}

func (s *Service) createConversation(r *http.Request, userID int64) (any, error) {
	var input struct {
		Name    string  `json:"name"`
		UserIDs []int64 `json:"user_ids"`
	}
	if err := decode(r, &input); err != nil {
		return nil, err
	}
	input.Name = strings.TrimSpace(input.Name)
	if len(input.UserIDs) < 1 || len(input.UserIDs) > 15 || utf8.RuneCountInString(input.Name) > 64 {
		return nil, ErrInvalid
	}
	tx, err := storage.BeginWrite(r.Context(), s.db)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	ids := []int64{userID}
	seen := map[int64]bool{userID: true}
	for _, id := range input.UserIDs {
		if id <= 0 || seen[id] {
			return nil, ErrInvalid
		}
		seen[id] = true
		if err = canContact(r.Context(), tx, userID, id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	var key any
	if len(ids) == 2 {
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		key = fmt.Sprintf("%d:%d", ids[0], ids[1])
	}
	var id int64
	err = tx.QueryRowContext(r.Context(), `INSERT INTO conversations(name,created_by,direct_key) VALUES(?,?,?) ON CONFLICT(direct_key) DO UPDATE SET direct_key=excluded.direct_key RETURNING id`, input.Name, userID, key).Scan(&id)
	if err != nil {
		return nil, err
	}
	for _, uid := range ids {
		if _, err = tx.ExecContext(r.Context(), `INSERT INTO conversation_members(conversation_id,user_id) VALUES(?,?) ON CONFLICT DO NOTHING`, id, uid); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	for _, uid := range ids {
		s.emit(uid, "conversation.changed", map[string]any{"conversation_id": id})
	}
	return map[string]int64{"id": id}, nil
}
func conversationAccess(ctx context.Context, q queryer, id, uid int64) error {
	var member bool
	err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM conversation_members WHERE conversation_id=? AND user_id=?)`, id, uid).Scan(&member)
	if err != nil {
		return err
	}
	if !member {
		return ErrForbidden
	}
	return nil
}
func (s *Service) conversationMessages(r *http.Request, userID int64) (any, error) {
	id := path(r, "conversationID")
	tx, err := s.db.BeginTx(r.Context(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = conversationAccess(r.Context(), tx, id, userID); err != nil {
		return nil, err
	}
	cursor := int64(1<<63 - 1)
	if raw := r.URL.Query().Get("before"); raw != "" {
		if _, err = fmt.Sscan(raw, &cursor); err != nil || cursor <= 0 {
			return nil, ErrInvalid
		}
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(query)) > 200 {
		return nil, ErrInvalid
	}
	rows, err := tx.QueryContext(r.Context(), `SELECT m.id,m.conversation_id,m.author_user_id,u.username,m.content,m.client_message_id,m.created_at,m.note,m.revision,m.forward_data FROM conversation_messages m JOIN users u ON u.id=m.author_user_id WHERE conversation_id=? AND m.id<? AND (?='' OR voxhold_contains(m.content||' '||m.note||' '||COALESCE((SELECT group_concat(a.filename,' ') FROM attachments a WHERE a.direct_message_id=m.id),''),?)) ORDER BY m.id DESC LIMIT 51`, id, cursor, query, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []DirectMessage{}
	for rows.Next() {
		var v DirectMessage
		var forward sql.NullString
		if err := rows.Scan(&v.ID, &v.ConversationID, &v.Author.ID, &v.Author.Username, &v.Content, &v.ClientMessageID, &v.CreatedAt, &v.Note, &v.Revision, &forward); err != nil {
			return nil, err
		}
		if forward.Valid {
			if err := json.Unmarshal([]byte(forward.String), &v.Forward); err != nil {
				return nil, err
			}
		}
		values = append(values, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Release the history cursor before the batched attachment read.
	rows.Close()
	ids := []int64{}
	byID := map[int64]*DirectMessage{}
	for i := range values {
		ids = append(ids, values[i].ID)
		byID[values[i].ID] = &values[i]
	}
	encoded, _ := json.Marshal(ids)
	attachments, err := tx.QueryContext(r.Context(), `SELECT direct_message_id,id,filename,mime_type,size_bytes,deleted_at IS NOT NULL FROM attachments WHERE direct_message_id IN (SELECT value FROM json_each(?)) ORDER BY id`, string(encoded))
	if err != nil {
		return nil, err
	}
	for attachments.Next() {
		var id int64
		var a message.Attachment
		if err = attachments.Scan(&id, &a.ID, &a.Filename, &a.MIMEType, &a.SizeBytes, &a.Deleted); err != nil {
			break
		}
		if v := byID[id]; v != nil {
			v.Attachments = append(v.Attachments, a)
		}
	}
	if err == nil {
		err = attachments.Err()
	}
	attachments.Close()
	if err != nil {
		return nil, err
	}
	if err = s.hydrateDirect(r.Context(), tx, userID, values); err != nil {
		return nil, err
	}
	more := len(values) > 50
	if more {
		values = values[:50]
	}
	return map[string]any{"messages": values, "has_more": more}, nil
}

type DirectInput struct {
	AttachmentIDs   []string                  `json:"attachment_ids"`
	Content         string                    `json:"content"`
	ClientMessageID string                    `json:"client_message_id"`
	Poll            *message.PollInput        `json:"poll,omitempty"`
	Forward         *message.ForwardReference `json:"-"`
}

func (s *Service) sendConversation(r *http.Request, userID int64) (any, error) {
	var input DirectInput
	if err := decode(r, &input); err != nil {
		return nil, err
	}
	return s.createDirect(r.Context(), path(r, "conversationID"), userID, input)
}
func (s *Service) createDirect(ctx context.Context, id, userID int64, input DirectInput) (any, error) {
	if input.Poll != nil {
		if err := input.Poll.Validate(); err != nil {
			return nil, ErrInvalid
		}
		if strings.TrimSpace(input.Content) == "" {
			input.Content = input.Poll.Question
		}
	}
	input.Content = strings.TrimSpace(input.Content)
	if len(input.AttachmentIDs) > 10 {
		return nil, ErrInvalid
	}
	if input.Content == "" && len(input.AttachmentIDs) > 0 {
		input.Content = "\U0001f4ce"
	}
	if strings.ContainsRune(input.Content, '\x00') || input.Content == "" || utf8.RuneCountInString(input.Content) > 5120 || len(input.ClientMessageID) < 1 || len(input.ClientMessageID) > 64 {
		return nil, ErrInvalid
	}
	tx, err := storage.BeginWrite(ctx, s.db)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = activeActor(ctx, tx, userID); err != nil {
		return nil, err
	}
	if err = conversationAccess(ctx, tx, id, userID); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT user_id FROM conversation_members WHERE conversation_id=?`, id)
	if err != nil {
		return nil, err
	}
	ids := []int64{}
	for rows.Next() {
		var uid int64
		if err = rows.Scan(&uid); err != nil {
			break
		}
		ids = append(ids, uid)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	var saved bool
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(saved_owner_id=?,0) FROM conversations WHERE id=?`, userID, id).Scan(&saved); err != nil {
		return nil, err
	}
	if len(ids) < 2 && !saved {
		return nil, ErrForbidden
	}
	for _, uid := range ids {
		if uid != userID {
			if err = canContact(ctx, tx, userID, uid); err != nil {
				return nil, err
			}
		}
	}
	var v DirectMessage
	v.ConversationID = id
	v.Author.ID = userID
	v.ClientMessageID = input.ClientMessageID
	encoded, _ := json.Marshal([]any{input.Content, input.AttachmentIDs})
	if input.Poll != nil || input.Forward != nil {
		encoded, _ = json.Marshal([]any{input.Content, input.AttachmentIDs, input.Poll, input.Forward})
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(encoded))
	var original string
	replayed := false
	err = tx.QueryRowContext(ctx, `SELECT id,content,created_at,revision,request_hash FROM conversation_messages WHERE conversation_id=? AND author_user_id=? AND client_message_id=?`, id, userID, input.ClientMessageID).Scan(&v.ID, &v.Content, &v.CreatedAt, &v.Revision, &original)
	if err == nil {
		replayed = true
		if original != hash {
			return nil, ErrConflict
		}
	} else if err == sql.ErrNoRows {
		err = tx.QueryRowContext(ctx, `INSERT INTO conversation_messages(conversation_id,author_user_id,content,client_message_id,request_hash,forward_data) VALUES(?,?,?,?,?,?) RETURNING id,content,created_at,revision`, id, userID, input.Content, input.ClientMessageID, hash, forwardData(input.Forward)).Scan(&v.ID, &v.Content, &v.CreatedAt, &v.Revision)
	}
	if err != nil {
		return nil, err
	}
	if !replayed {
		if err = message.StorePoll(ctx, tx, "conversation", v.ID, input.Poll); err != nil {
			return nil, ErrInvalid
		}
		for _, aid := range input.AttachmentIDs {
			result, err := tx.ExecContext(ctx, `UPDATE attachments SET direct_message_id=? WHERE id=? AND conversation_id=? AND owner_user_id=? AND direct_message_id IS NULL AND deleted_at IS NULL`, v.ID, aid, id, userID)
			if err != nil {
				return nil, err
			}
			n, _ := result.RowsAffected()
			if n != 1 {
				return nil, ErrInvalid
			}
		}
	}
	// Return and broadcast the same complete message as history, including on
	// retries after the original HTTP response was lost.
	attached, err := tx.QueryContext(ctx, `SELECT id,filename,mime_type,size_bytes,deleted_at IS NOT NULL FROM attachments WHERE direct_message_id=? ORDER BY id`, v.ID)
	if err != nil {
		return nil, err
	}
	for attached.Next() {
		var a message.Attachment
		if err = attached.Scan(&a.ID, &a.Filename, &a.MIMEType, &a.SizeBytes, &a.Deleted); err != nil {
			break
		}
		v.Attachments = append(v.Attachments, a)
	}
	if err == nil {
		err = attached.Err()
	}
	attached.Close()
	if err != nil {
		return nil, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT username FROM users WHERE id=?`, userID).Scan(&v.Author.Username); err != nil {
		return nil, err
	}
	v.Forward = input.Forward
	values := []DirectMessage{v}
	if err = s.hydrateDirect(ctx, tx, userID, values); err != nil {
		return nil, err
	}
	v = values[0]
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	if !replayed {
		for _, uid := range ids {
			s.emit(uid, "conversation.message", v)
			if uid != userID {
				s.notifyConversation(ctx, uid, v)
			}
		}
	}
	return v, nil
}
func (s *Service) emit(uid int64, event string, v any) {
	if s.notify != nil {
		s.notify(uid, event, v)
	}
}
func (s *Service) readConversation(r *http.Request, userID int64) (any, error) {
	var input struct {
		MessageID int64 `json:"message_id"`
	}
	if err := decode(r, &input); err != nil {
		return nil, err
	}
	id := path(r, "conversationID")
	if err := conversationAccess(r.Context(), s.db, id, userID); err != nil {
		return nil, err
	}
	result, err := s.db.ExecContext(r.Context(), `UPDATE conversation_members SET last_read_id=max(last_read_id,?) WHERE conversation_id=? AND user_id=? AND EXISTS(SELECT 1 FROM conversation_messages WHERE id=? AND conversation_id=?)`, input.MessageID, id, userID, input.MessageID, id)
	if err != nil {
		return nil, err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return nil, ErrInvalid
	}
	s.emit(userID, "conversation.read", map[string]any{"conversation_id": id, "message_id": input.MessageID})
	return input, nil
}
func (s *Service) leaveConversation(r *http.Request, userID int64) (any, error) {
	_, err := s.db.ExecContext(r.Context(), `DELETE FROM conversation_members WHERE conversation_id=? AND user_id=?`, path(r, "conversationID"), userID)
	return map[string]bool{"ok": true}, err
}

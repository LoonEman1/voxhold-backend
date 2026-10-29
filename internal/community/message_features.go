package community

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"voxhold-backend/internal/message"
)

func (s *Service) SetMessages(service *message.Service, copier func(context.Context, int64, string, string, int64, string) (string, error)) {
	s.messages = service
	s.copyAttachment = copier
}
func (s *Service) registerFeatures(mux *http.ServeMux, auth func(http.Handler) http.Handler) {
	routes := map[string]endpoint{
		"GET /api/v1/me/favorites":                                                             s.favorites,
		"PUT /api/v1/conversations/{conversationID}/notifications":                             s.conversationSettings,
		"GET /api/v1/conversations/{conversationID}/notifications":                             s.conversationSettings,
		"PATCH /api/v1/conversations/{conversationID}/messages/{messageID}/note":               s.savedNote,
		"DELETE /api/v1/conversations/{conversationID}/messages/{messageID}":                   s.deleteSaved,
		"PUT /api/v1/conversations/{conversationID}/messages/{messageID}/reactions/{emoji}":    s.directReaction,
		"DELETE /api/v1/conversations/{conversationID}/messages/{messageID}/reactions/{emoji}": s.directReaction,
		"GET /api/v1/conversations/{conversationID}/messages/{messageID}/reactions/{emoji}":    s.directReactors,
		"POST /api/v1/me/forward":                                     s.forwardMessage,
		"GET /api/v1/me/forward-destinations":                         s.forwardDestinations,
		"GET /api/v1/me/scheduled-messages":                           s.scheduled,
		"POST /api/v1/me/scheduled-messages":                          s.schedule,
		"PUT /api/v1/me/scheduled-messages/{scheduleID}":              s.reschedule,
		"DELETE /api/v1/me/scheduled-messages/{scheduleID}":           s.cancelSchedule,
		"PUT /api/v1/polls/{pollID}/votes":                            s.vote,
		"GET /api/v1/polls/{pollID}":                                  s.getPoll,
		"GET /api/v1/servers/{serverID}/events":                       s.events,
		"POST /api/v1/servers/{serverID}/events":                      s.saveEvent,
		"PUT /api/v1/servers/{serverID}/events/{eventID}":             s.saveEvent,
		"DELETE /api/v1/servers/{serverID}/events/{eventID}":          s.cancelEvent,
		"PUT /api/v1/servers/{serverID}/events/{eventID}/response":    s.eventResponse,
		"DELETE /api/v1/servers/{serverID}/events/{eventID}/response": s.eventResponse,
		"GET /api/v1/servers/{serverID}/audit":                        s.auditLog,
		"GET /api/v1/conversations/{conversationID}/call":             s.callState,
		"PUT /api/v1/conversations/{conversationID}/call":             s.joinCall,
		"DELETE /api/v1/conversations/{conversationID}/call":          s.leaveCall,
		"POST /api/v1/conversations/{conversationID}/call/signal":     s.callSignal,
	}
	for pattern, fn := range routes {
		mux.Handle(pattern, auth(s.wrap(fn)))
	}
}
func forwardData(v *message.ForwardReference) any {
	if v == nil {
		return nil
	}
	b, _ := json.Marshal(v)
	return string(b)
}
func (s *Service) hydrateDirect(ctx context.Context, tx *sql.Tx, uid int64, values []DirectMessage) error {
	ids := []int64{}
	byID := map[int64]*DirectMessage{}
	for i := range values {
		v := &values[i]
		ids = append(ids, v.ID)
		byID[v.ID] = v
		v.Reactions = []message.Reaction{}
		v.ReactionsRevision = v.Revision
	}
	polls, err := message.LoadPolls(ctx, tx, uid, "conversation", ids)
	if err != nil {
		return err
	}
	for i := range values {
		values[i].Poll = polls[values[i].ID]
	}
	encoded, _ := json.Marshal(ids)
	rows, err := tx.QueryContext(ctx, `SELECT message_id,emoji,count(*),max(user_id=?) FROM conversation_reactions WHERE message_id IN(SELECT value FROM json_each(?)) GROUP BY message_id,emoji ORDER BY emoji`, uid, string(encoded))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var r message.Reaction
		if err = rows.Scan(&id, &r.Emoji, &r.Count, &r.Me); err != nil {
			return err
		}
		byID[id].Reactions = append(byID[id].Reactions, r)
	}
	return rows.Err()
}
func (s *Service) favorites(r *http.Request, uid int64) (any, error) {
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var id int64
	err = tx.QueryRowContext(r.Context(), `INSERT INTO conversations(name,created_by,saved_owner_id) VALUES('Избранное',?,?) ON CONFLICT(saved_owner_id) WHERE saved_owner_id IS NOT NULL DO UPDATE SET saved_owner_id=excluded.saved_owner_id RETURNING id`, uid, uid).Scan(&id)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO conversation_members(conversation_id,user_id) VALUES(?,?) ON CONFLICT DO NOTHING`, id, uid); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return Conversation{ID: id, Name: "Избранное", Saved: true, Members: []Person{}}, nil
}
func (s *Service) conversationSettings(r *http.Request, uid int64) (any, error) {
	cid := path(r, "conversationID")
	if err := conversationAccess(r.Context(), s.db, cid, uid); err != nil {
		return nil, err
	}
	var muted bool
	if r.Method == "PUT" {
		var v struct {
			Muted bool `json:"muted"`
		}
		if err := decode(r, &v); err != nil {
			return nil, err
		}
		muted = v.Muted
		if _, err := s.db.ExecContext(r.Context(), `UPDATE conversation_members SET muted=? WHERE conversation_id=? AND user_id=?`, muted, cid, uid); err != nil {
			return nil, err
		}
		s.emit(uid, "conversation.settings", map[string]any{"conversation_id": cid, "muted": muted})
	} else {
		if err := s.db.QueryRowContext(r.Context(), `SELECT muted FROM conversation_members WHERE conversation_id=? AND user_id=?`, cid, uid).Scan(&muted); err != nil {
			return nil, err
		}
	}
	return map[string]any{"conversation_id": cid, "muted": muted}, nil
}
func (s *Service) savedNote(r *http.Request, uid int64) (any, error) {
	var v struct {
		Note string `json:"note"`
	}
	if err := decode(r, &v); err != nil {
		return nil, err
	}
	if len([]rune(v.Note)) > 2000 {
		return nil, ErrInvalid
	}
	res, err := s.db.ExecContext(r.Context(), `UPDATE conversation_messages SET note=?,revision=revision+1 WHERE id=? AND conversation_id=? AND EXISTS(SELECT 1 FROM conversations WHERE id=conversation_messages.conversation_id AND saved_owner_id=?)`, v.Note, path(r, "messageID"), path(r, "conversationID"), uid)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return nil, ErrForbidden
	}
	s.emit(uid, "conversation.changed", map[string]int64{"conversation_id": path(r, "conversationID")})
	return v, nil
}
func (s *Service) deleteSaved(r *http.Request, uid int64) (any, error) {
	res, err := s.db.ExecContext(r.Context(), `DELETE FROM conversation_messages WHERE id=? AND conversation_id=? AND EXISTS(SELECT 1 FROM conversations WHERE id=conversation_messages.conversation_id AND saved_owner_id=?)`, path(r, "messageID"), path(r, "conversationID"), uid)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return nil, ErrForbidden
	}
	s.emit(uid, "conversation.deleted", map[string]int64{"conversation_id": path(r, "conversationID"), "message_id": path(r, "messageID")})
	return map[string]bool{"ok": true}, nil
}
func (s *Service) directReaction(r *http.Request, uid int64) (any, error) {
	cid, mid := path(r, "conversationID"), path(r, "messageID")
	emoji := r.PathValue("emoji")
	if !message.ValidReaction(emoji) {
		return nil, ErrInvalid
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = conversationAccess(r.Context(), tx, cid, uid); err != nil {
		return nil, err
	}
	var yes bool
	if err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM conversation_messages WHERE id=? AND conversation_id=?)`, mid, cid).Scan(&yes); err != nil {
		return nil, err
	}
	if !yes {
		return nil, sql.ErrNoRows
	}
	if r.Method == "PUT" {
		var count int
		err = tx.QueryRowContext(r.Context(), `SELECT count(DISTINCT emoji) FROM conversation_reactions WHERE message_id=? AND emoji<>?`, mid, emoji).Scan(&count)
		if err != nil {
			return nil, err
		}
		if count >= 20 {
			return nil, ErrInvalid
		}
		_, err = tx.ExecContext(r.Context(), `INSERT INTO conversation_reactions(message_id,user_id,emoji) VALUES(?,?,?) ON CONFLICT DO NOTHING`, mid, uid, emoji)
	} else {
		_, err = tx.ExecContext(r.Context(), `DELETE FROM conversation_reactions WHERE message_id=? AND user_id=? AND emoji=?`, mid, uid, emoji)
	}
	if err != nil {
		return nil, err
	}
	var revision int64
	if err = tx.QueryRowContext(r.Context(), `SELECT revision FROM conversation_messages WHERE id=?`, mid).Scan(&revision); err != nil {
		return nil, err
	}
	values := []DirectMessage{{ID: mid, Revision: revision}}
	if err = s.hydrateDirect(r.Context(), tx, uid, values); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	s.emitConversation(r.Context(), cid, "conversation.reactions", map[string]int64{"conversation_id": cid, "message_id": mid})
	return map[string]any{"reactions": values[0].Reactions, "revision": revision}, nil
}
func (s *Service) directReactors(r *http.Request, uid int64) (any, error) {
	cid, mid := path(r, "conversationID"), path(r, "messageID")
	if err := conversationAccess(r.Context(), s.db, cid, uid); err != nil {
		return nil, err
	}
	emoji := r.PathValue("emoji")
	if !message.ValidReaction(emoji) {
		return nil, ErrInvalid
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	rows, err := s.db.QueryContext(r.Context(), `SELECT u.id,u.username FROM conversation_reactions v JOIN users u ON u.id=v.user_id JOIN conversation_messages m ON m.id=v.message_id WHERE m.conversation_id=? AND m.id=? AND v.emoji=? AND u.id>? ORDER BY u.id LIMIT 51`, cid, mid, emoji, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	v := message.ReactorPage{Users: []message.Author{}}
	for rows.Next() {
		var a message.Author
		if err = rows.Scan(&a.UserID, &a.Username); err != nil {
			return nil, err
		}
		v.Users = append(v.Users, a)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(v.Users) > 50 {
		v.HasMore = true
		v.Users = v.Users[:50]
		id := v.Users[49].UserID
		v.NextUserID = &id
	}
	return v, nil
}
func (s *Service) emitConversation(ctx context.Context, cid int64, event string, data any) {
	rows, err := s.db.QueryContext(ctx, `SELECT user_id FROM conversation_members WHERE conversation_id=?`, cid)
	if err != nil {
		return
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		s.emit(id, event, data)
	}
}
func (s *Service) notifyConversation(ctx context.Context, uid int64, v DirectMessage) {
	var muted bool
	if err := s.db.QueryRowContext(ctx, `SELECT muted FROM conversation_members WHERE conversation_id=? AND user_id=?`, v.ConversationID, uid).Scan(&muted); err == nil && !muted {
		s.emit(uid, "conversation.notification", v)
	}
}

type Destination struct {
	Kind     string `json:"kind"`
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	ServerID int64  `json:"server_id,omitempty"`
}

func (s *Service) forwardDestinations(r *http.Request, uid int64) (any, error) {
	if _, err := s.favorites(r, uid); err != nil {
		return nil, err
	}
	values := []Destination{}
	rows, err := s.db.QueryContext(r.Context(), `SELECT c.id,c.server_id,s.name||' / '||c.name FROM channels c JOIN servers s ON s.id=c.server_id JOIN channel_accessible_members a ON a.channel_id=c.id AND a.user_id=? JOIN member_permissions p ON p.server_id=c.server_id AND p.user_id=a.user_id WHERE (p.permissions&2)<>0 AND (c.is_news=0 OR EXISTS(SELECT 1 FROM server_members WHERE server_id=c.server_id AND user_id=? AND role IN('owner','admin')) OR EXISTS(SELECT 1 FROM channel_publishers WHERE channel_id=c.id AND user_id=?)) ORDER BY s.name,c.position,c.id`, uid, uid, uid)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		v := Destination{Kind: "channel"}
		if err = rows.Scan(&v.ID, &v.ServerID, &v.Name); err != nil {
			break
		}
		values = append(values, v)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = s.db.QueryContext(r.Context(), `SELECT c.id,CASE WHEN c.saved_owner_id=? THEN 'Избранное' ELSE COALESCE(NULLIF(c.name,''),(SELECT group_concat(u.username,', ') FROM conversation_members m JOIN users u ON u.id=m.user_id WHERE m.conversation_id=c.id AND u.id<>?)) END FROM conversations c JOIN conversation_members a ON a.conversation_id=c.id WHERE a.user_id=? ORDER BY c.saved_owner_id IS NOT NULL DESC,c.id DESC`, uid, uid, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		v := Destination{Kind: "conversation"}
		if err = rows.Scan(&v.ID, &v.Name); err != nil {
			return nil, err
		}
		values = append(values, v)
	}
	return values, rows.Err()
}

type Source struct {
	Kind string `json:"kind"`
	ID   int64  `json:"id"`
}
type sourceSnapshot struct {
	Content                             string
	Forward                             *message.ForwardReference
	AttachmentIDs                       []string
	ChannelID, ServerID, ConversationID int64
}

func (s *Service) snapshot(ctx context.Context, uid int64, src Source) (sourceSnapshot, error) {
	var v sourceSnapshot
	var stored sql.NullString
	v.Forward = &message.ForwardReference{}
	var err error
	if src.Kind == "channel" {
		err = s.db.QueryRowContext(ctx, `SELECT m.content,m.author_user_id,u.username,m.created_at,m.forward_data,m.channel_id,c.server_id FROM messages m JOIN channels c ON c.id=m.channel_id JOIN users u ON u.id=m.author_user_id WHERE m.id=? AND EXISTS(SELECT 1 FROM channel_accessible_members a WHERE a.channel_id=c.id AND a.user_id=?)`, src.ID, uid).Scan(&v.Content, &v.Forward.Author.UserID, &v.Forward.Author.Username, &v.Forward.CreatedAt, &stored, &v.ChannelID, &v.ServerID)
	} else if src.Kind == "conversation" {
		err = s.db.QueryRowContext(ctx, `SELECT m.content,m.author_user_id,u.username,m.created_at,m.forward_data,m.conversation_id FROM conversation_messages m JOIN users u ON u.id=m.author_user_id WHERE m.id=? AND EXISTS(SELECT 1 FROM conversation_members WHERE conversation_id=m.conversation_id AND user_id=?)`, src.ID, uid).Scan(&v.Content, &v.Forward.Author.UserID, &v.Forward.Author.Username, &v.Forward.CreatedAt, &stored, &v.ConversationID)
	} else {
		return v, ErrInvalid
	}
	if err == sql.ErrNoRows {
		return v, ErrForbidden
	}
	if err != nil {
		return v, err
	}
	if stored.Valid {
		if err = json.Unmarshal([]byte(stored.String), v.Forward); err != nil {
			return v, err
		}
	}
	column := "message_id"
	if src.Kind == "conversation" {
		column = "direct_message_id"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM attachments WHERE `+column+`=? AND deleted_at IS NULL ORDER BY id`, src.ID)
	if err != nil {
		return v, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return v, err
		}
		v.AttachmentIDs = append(v.AttachmentIDs, id)
	}
	return v, rows.Err()
}
func (s *Service) destination(ctx context.Context, uid int64, d Destination) (int64, error) {
	if d.ID <= 0 {
		return 0, ErrInvalid
	}
	if d.Kind == "conversation" {
		return 0, CanSend(ctx, s.db, d.ID, uid)
	}
	if d.Kind != "channel" {
		return 0, ErrInvalid
	}
	var sid int64
	err := s.db.QueryRowContext(ctx, `SELECT c.server_id FROM channels c JOIN channel_accessible_members a ON a.channel_id=c.id AND a.user_id=? JOIN member_permissions p ON p.server_id=c.server_id AND p.user_id=a.user_id WHERE c.id=? AND (p.permissions&2)<>0 AND (c.is_news=0 OR EXISTS(SELECT 1 FROM server_members WHERE server_id=c.server_id AND user_id=? AND role IN('owner','admin')) OR EXISTS(SELECT 1 FROM channel_publishers WHERE channel_id=c.id AND user_id=?))`, uid, d.ID, uid, uid).Scan(&sid)
	if err == sql.ErrNoRows {
		err = ErrForbidden
	}
	return sid, err
}
func (s *Service) forwardMessage(r *http.Request, uid int64) (any, error) {
	var input struct {
		Source      Source      `json:"source"`
		Destination Destination `json:"destination"`
		Key         string      `json:"client_message_id"`
	}
	if err := decode(r, &input); err != nil {
		return nil, err
	}
	if input.Key == "" || len(input.Key) > 64 {
		return nil, ErrInvalid
	}
	sid, err := s.destination(r.Context(), uid, input.Destination)
	if err != nil {
		return nil, err
	}
	src, err := s.snapshot(r.Context(), uid, input.Source)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for i, id := range src.AttachmentIDs {
		if s.copyAttachment == nil {
			return nil, ErrInvalid
		}
		hash := sha256.Sum256([]byte(fmt.Sprintf("%d:%s:%d:%s:%d:%s", uid, input.Key, i, input.Destination.Kind, input.Destination.ID, id)))
		copyID := fmt.Sprintf("%x", hash[:16])
		copied, err := s.copyAttachment(r.Context(), uid, id, input.Destination.Kind, input.Destination.ID, copyID)
		if err != nil {
			return nil, err
		}
		ids = append(ids, copied)
	}
	if input.Destination.Kind == "conversation" {
		return s.createDirect(r.Context(), input.Destination.ID, uid, DirectInput{Content: src.Content, ClientMessageID: input.Key, AttachmentIDs: ids, Forward: src.Forward})
	}
	if s.messages == nil {
		return nil, ErrInvalid
	}
	return s.messages.Forward(r.Context(), sid, input.Destination.ID, uid, message.ForwardInput{SourceServerID: src.ServerID, SourceChannelID: src.ChannelID, SourceConversationID: src.ConversationID, SourceMessageID: input.Source.ID, ClientMessageID: input.Key, AttachmentIDs: ids})
}
func (s *Service) pollAccess(ctx context.Context, tx *sql.Tx, pid, uid int64) (string, int64, int64, int64, error) {
	var channel, direct sql.NullInt64
	var closes int64
	err := tx.QueryRowContext(ctx, `SELECT channel_message_id,direct_message_id,closes_at FROM polls WHERE id=?`, pid).Scan(&channel, &direct, &closes)
	if err != nil {
		return "", 0, 0, 0, err
	}
	var scope int64
	if channel.Valid {
		err = tx.QueryRowContext(ctx, `SELECT channel_id FROM messages WHERE id=?`, channel.Int64).Scan(&scope)
		if err == nil {
			err = accessible(ctx, tx, scope, uid)
		}
		return "channel", channel.Int64, scope, closes, err
	}
	err = tx.QueryRowContext(ctx, `SELECT conversation_id FROM conversation_messages WHERE id=?`, direct.Int64).Scan(&scope)
	if err == nil {
		err = conversationAccess(ctx, tx, scope, uid)
	}
	return "conversation", direct.Int64, scope, closes, err
}
func (s *Service) getPoll(r *http.Request, uid int64) (any, error) {
	tx, err := s.db.BeginTx(r.Context(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	kind, mid, _, _, err := s.pollAccess(r.Context(), tx, path(r, "pollID"), uid)
	if err != nil {
		return nil, err
	}
	polls, err := message.LoadPolls(r.Context(), tx, uid, kind, []int64{mid})
	return polls[mid], err
}

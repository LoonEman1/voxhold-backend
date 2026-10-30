package community

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
)

// The array response is backward compatible. A full page continues with the
// last row's (last_message_id,id); a conversation can also be opened by ID.
func (s *Service) conversations(r *http.Request, uid int64) (any, error) {
	return s.listConversations(r, uid, 0)
}
func (s *Service) conversation(r *http.Request, uid int64) (any, error) {
	id := path(r, "conversationID")
	if id <= 0 {
		return nil, ErrInvalid
	}
	values, err := s.listConversations(r, uid, id)
	if err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return nil, ErrForbidden
	}
	return values[0], nil
}
func (s *Service) listConversations(r *http.Request, uid, only int64) ([]Conversation, error) {
	var beforeActivity, beforeID int64
	if raw := r.URL.Query().Get("before_id"); raw != "" {
		var err error
		beforeID, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || beforeID <= 0 {
			return nil, ErrInvalid
		}
		beforeActivity, err = strconv.ParseInt(r.URL.Query().Get("before_activity"), 10, 64)
		if err != nil || beforeActivity < 0 {
			return nil, ErrInvalid
		}
	}
	if only < 0 {
		return nil, ErrInvalid
	}
	tx, err := s.db.BeginTx(r.Context(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(r.Context(), `WITH listed AS (
SELECT c.id,c.name,c.direct_key IS NOT NULL AS direct,a.muted,c.saved_owner_id IS NOT NULL AS saved,
(SELECT count(*) FROM conversation_messages m WHERE m.conversation_id=c.id AND m.id>a.last_read_id AND m.author_user_id<>?) AS unread,
COALESCE((SELECT max(id) FROM conversation_messages WHERE conversation_id=c.id),0) AS activity
FROM conversations c JOIN conversation_members a ON a.conversation_id=c.id WHERE a.user_id=? AND (?=0 OR c.id=?))
SELECT id,name,direct,muted,saved,unread,activity FROM listed
WHERE (?=0 OR activity<? OR (activity=? AND id<?)) ORDER BY activity DESC,id DESC LIMIT 200`, uid, uid, only, only, beforeID, beforeActivity, beforeActivity, beforeID)
	if err != nil {
		return nil, err
	}
	values := []Conversation{}
	ids := []int64{}
	for rows.Next() {
		v := Conversation{Members: []Person{}}
		if err = rows.Scan(&v.ID, &v.Name, &v.Direct, &v.Muted, &v.Saved, &v.Unread, &v.LastMessageID); err != nil {
			break
		}
		ids = append(ids, v.ID)
		values = append(values, v)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	positions := map[int64]int{}
	for i := range values {
		positions[values[i].ID] = i
	}
	encoded, _ := json.Marshal(ids)
	rows, err = tx.QueryContext(r.Context(), `SELECT a.conversation_id,u.id,u.username FROM conversation_members a JOIN users u ON u.id=a.user_id WHERE a.conversation_id IN(SELECT value FROM json_each(?)) ORDER BY a.conversation_id,u.id`, string(encoded))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var cid int64
		var p Person
		if err = rows.Scan(&cid, &p.ID, &p.Username); err != nil {
			break
		}
		i := positions[cid]
		values[i].Members = append(values[i].Members, p)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	return values, tx.Commit()
}

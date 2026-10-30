package community

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

type pageRequest struct {
	paged      bool
	limit      int
	before, id int64
}

func parsePage(r *http.Request) (pageRequest, error) {
	q := r.URL.Query()
	p := pageRequest{paged: q.Has("limit") || q.Has("before"), limit: 200}
	if q.Has("limit") {
		n, err := strconv.Atoi(q.Get("limit"))
		if err != nil || n < 1 || n > 200 {
			return p, ErrInvalid
		}
		p.limit = n
	}
	if q.Has("before") {
		raw, err := base64.RawURLEncoding.DecodeString(q.Get("before"))
		if err != nil || len(raw) > 64 {
			return p, ErrInvalid
		}
		a, b, ok := strings.Cut(string(raw), ":")
		if !ok {
			return p, ErrInvalid
		}
		p.before, err = strconv.ParseInt(a, 10, 64)
		if err != nil || p.before < 0 {
			return p, ErrInvalid
		}
		p.id, err = strconv.ParseInt(b, 10, 64)
		if err != nil || p.id < 1 {
			return p, ErrInvalid
		}
	}
	return p, nil
}
func pageCursor(value, id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%d:%d", value, id)))
}

func (s *Service) conversations(r *http.Request, uid int64) (any, error) {
	p, err := parsePage(r)
	if err != nil {
		return nil, err
	}
	values, activity, err := s.listConversations(r.Context(), uid, 0, p)
	if err != nil {
		return nil, err
	}
	more := len(values) > p.limit
	if more {
		values = values[:p.limit]
	}
	if !p.paged {
		return values, nil
	} // Retain the array contract for older clients.
	var next any
	if more {
		next = pageCursor(activity[p.limit-1], values[p.limit-1].ID)
	}
	return map[string]any{"conversations": values, "has_more": more, "next_cursor": next}, nil
}
func (s *Service) conversation(r *http.Request, uid int64) (any, error) {
	id := path(r, "conversationID")
	if id < 1 {
		return nil, ErrInvalid
	}
	values, _, err := s.listConversations(r.Context(), uid, id, pageRequest{limit: 1})
	if err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return nil, sql.ErrNoRows
	}
	return values[0], nil
}
func (s *Service) listConversations(ctx context.Context, uid, id int64, p pageRequest) ([]Conversation, []int64, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `WITH chats AS(
 SELECT c.id,c.name,c.direct_key IS NOT NULL AS direct,a.muted,c.saved_owner_id IS NOT NULL AS saved,
 (SELECT count(*) FROM conversation_messages m WHERE m.conversation_id=c.id AND m.id>a.last_read_id AND m.author_user_id<>?) AS unread,
 COALESCE((SELECT max(id) FROM conversation_messages WHERE conversation_id=c.id),0) AS activity
 FROM conversations c JOIN conversation_members a ON a.conversation_id=c.id WHERE a.user_id=?)
 SELECT id,name,direct,muted,saved,unread,activity FROM chats WHERE (?=0 OR id=?)
 AND (?=0 OR activity<? OR (activity=? AND id<?)) ORDER BY activity DESC,id DESC LIMIT ?`, uid, uid, id, id, p.id, p.before, p.before, p.id, p.limit+1)
	if err != nil {
		return nil, nil, err
	}
	values := []Conversation{}
	activity := []int64{}
	ids := []int64{}
	for rows.Next() {
		v := Conversation{Members: []Person{}}
		var at int64
		if err = rows.Scan(&v.ID, &v.Name, &v.Direct, &v.Muted, &v.Saved, &v.Unread, &at); err != nil {
			break
		}
		values = append(values, v)
		activity = append(activity, at)
		ids = append(ids, v.ID)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, nil, err
	}
	encoded, _ := json.Marshal(ids)
	rows, err = tx.QueryContext(ctx, `SELECT a.conversation_id,u.id,u.username FROM conversation_members a JOIN users u ON u.id=a.user_id WHERE a.conversation_id IN(SELECT value FROM json_each(?)) ORDER BY a.conversation_id,u.id`, string(encoded))
	if err != nil {
		return nil, nil, err
	}
	byID := map[int64]*Conversation{}
	for i := range values {
		byID[values[i].ID] = &values[i]
	}
	for rows.Next() {
		var cid int64
		var member Person
		if err = rows.Scan(&cid, &member.ID, &member.Username); err != nil {
			break
		}
		byID[cid].Members = append(byID[cid].Members, member)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, nil, err
	}
	return values, activity, tx.Commit()
}

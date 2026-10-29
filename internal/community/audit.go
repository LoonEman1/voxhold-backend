package community

import (
	"encoding/json"
	"net/http"
	"strconv"
)

type AuditEntry struct {
	ID         int64           `json:"id"`
	ActorID    *int64          `json:"actor_user_id"`
	Actor      string          `json:"actor_name"`
	Action     string          `json:"action"`
	TargetType string          `json:"target_type"`
	TargetID   int64           `json:"target_id"`
	TargetName string          `json:"target_name"`
	Details    json.RawMessage `json:"details"`
	CreatedAt  int64           `json:"created_at"`
}

func (s *Service) auditLog(r *http.Request, uid int64) (any, error) {
	sid := path(r, "serverID")
	var allowed bool
	if err := s.db.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM server_members WHERE server_id=? AND user_id=? AND role IN('owner','admin'))`, sid, uid).Scan(&allowed); err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrForbidden
	}
	before := int64(1<<63 - 1)
	if raw := r.URL.Query().Get("before"); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || v <= 0 {
			return nil, ErrInvalid
		}
		before = v
	}
	actor := int64(0)
	if raw := r.URL.Query().Get("actor_id"); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || v <= 0 {
			return nil, ErrInvalid
		}
		actor = v
	}
	action := r.URL.Query().Get("action")
	if len(action) > 80 {
		return nil, ErrInvalid
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT id,actor_user_id,actor_name,action,target_type,target_id,target_name,details,created_at FROM admin_audit WHERE server_id=? AND id<? AND (?=0 OR actor_user_id=?) AND (?='' OR action=?) ORDER BY id DESC LIMIT 51`, sid, before, actor, actor, action, action)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []AuditEntry{}
	for rows.Next() {
		var v AuditEntry
		var data string
		if err = rows.Scan(&v.ID, &v.ActorID, &v.Actor, &v.Action, &v.TargetType, &v.TargetID, &v.TargetName, &data, &v.CreatedAt); err != nil {
			return nil, err
		}
		v.Details = json.RawMessage(data)
		values = append(values, v)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	more := len(values) > 50
	if more {
		values = values[:50]
	}
	return map[string]any{"items": values, "has_more": more}, nil
}

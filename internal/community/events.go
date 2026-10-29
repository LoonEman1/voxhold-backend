package community

import (
	"context"
	"net/http"
	"strings"
	"time"
	_ "time/tzdata"
)

type ServerEvent struct {
	ID          int64  `json:"id"`
	ServerID    int64  `json:"server_id"`
	CreatorID   int64  `json:"creator_user_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	StartsAt    int64  `json:"starts_at"`
	Timezone    string `json:"timezone"`
	Cancelled   bool   `json:"cancelled"`
	Revision    int64  `json:"revision"`
	Going       int    `json:"going"`
	Maybe       int    `json:"maybe"`
	Response    string `json:"response"`
}

func serverMember(ctx context.Context, q queryer, sid, uid int64) error {
	var yes bool
	err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM member_permissions WHERE server_id=? AND user_id=? AND (permissions&1)<>0)`, sid, uid).Scan(&yes)
	if err != nil {
		return err
	}
	if !yes {
		return ErrForbidden
	}
	return nil
}
func (s *Service) emitServer(ctx context.Context, sid int64, event string, v any) {
	rows, err := s.db.QueryContext(ctx, `SELECT user_id FROM server_members WHERE server_id=?`, sid)
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
		s.emit(id, event, v)
	}
}
func (s *Service) emitChannel(ctx context.Context, cid int64, event string, v any) {
	rows, err := s.db.QueryContext(ctx, `SELECT user_id FROM channel_accessible_members WHERE channel_id=?`, cid)
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
		s.emit(id, event, v)
	}
}
func (s *Service) events(r *http.Request, uid int64) (any, error) {
	sid := path(r, "serverID")
	if err := serverMember(r.Context(), s.db, sid, uid); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT e.id,e.server_id,e.creator_user_id,e.title,e.description,e.starts_at,e.timezone,e.cancelled,e.revision,(SELECT count(*) FROM server_event_responses WHERE event_id=e.id AND response='going'),(SELECT count(*) FROM server_event_responses WHERE event_id=e.id AND response='maybe'),COALESCE((SELECT response FROM server_event_responses WHERE event_id=e.id AND user_id=?),'') FROM server_events e WHERE e.server_id=? AND e.starts_at>unixepoch()-86400 ORDER BY e.starts_at,e.id LIMIT 100`, uid, sid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []ServerEvent{}
	for rows.Next() {
		var v ServerEvent
		if err = rows.Scan(&v.ID, &v.ServerID, &v.CreatorID, &v.Title, &v.Description, &v.StartsAt, &v.Timezone, &v.Cancelled, &v.Revision, &v.Going, &v.Maybe, &v.Response); err != nil {
			return nil, err
		}
		values = append(values, v)
	}
	return values, rows.Err()
}
func eventManage(ctx context.Context, q queryer, sid, eid, uid int64) error {
	if err := serverMember(ctx, q, sid, uid); err != nil {
		return err
	}
	var yes bool
	err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM server_events e WHERE e.id=? AND e.server_id=? AND (e.creator_user_id=? OR EXISTS(SELECT 1 FROM server_members WHERE server_id=e.server_id AND user_id=? AND role IN('owner','admin'))))`, eid, sid, uid, uid).Scan(&yes)
	if err != nil {
		return err
	}
	if !yes {
		return ErrForbidden
	}
	return nil
}
func (s *Service) saveEvent(r *http.Request, uid int64) (any, error) {
	var v ServerEvent
	if err := decode(r, &v); err != nil {
		return nil, err
	}
	v.Title = strings.TrimSpace(v.Title)
	if v.Title == "" || len([]rune(v.Title)) > 150 || len([]rune(v.Description)) > 3000 || v.StartsAt <= time.Now().Unix() || v.StartsAt > time.Now().AddDate(1, 0, 0).Unix() {
		return nil, ErrInvalid
	}
	if _, err := time.LoadLocation(v.Timezone); err != nil {
		return nil, ErrInvalid
	}
	sid, eid := path(r, "serverID"), path(r, "eventID")
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = serverMember(r.Context(), tx, sid, uid); err != nil {
		return nil, err
	}
	if eid == 0 {
		var allowed bool
		if err = tx.QueryRowContext(r.Context(), `SELECT (permissions&2)<>0 FROM member_permissions WHERE server_id=? AND user_id=?`, sid, uid).Scan(&allowed); err != nil {
			return nil, err
		}
		if !allowed {
			return nil, ErrForbidden
		}
		err = tx.QueryRowContext(r.Context(), `INSERT INTO server_events(server_id,creator_user_id,title,description,starts_at,timezone) VALUES(?,?,?,?,?,?) RETURNING id`, sid, uid, v.Title, v.Description, v.StartsAt, v.Timezone).Scan(&eid)
	} else {
		if err = eventManage(r.Context(), tx, sid, eid, uid); err != nil {
			return nil, err
		}
		result, updateErr := tx.ExecContext(r.Context(), `UPDATE server_events SET title=?,description=?,starts_at=?,timezone=?,revision=revision+1 WHERE id=? AND server_id=? AND cancelled=0`, v.Title, v.Description, v.StartsAt, v.Timezone, eid, sid)
		err = updateErr
		if err == nil {
			if n, countErr := result.RowsAffected(); countErr != nil {
				return nil, countErr
			} else if n != 1 {
				return nil, ErrConflict
			}

			_, err = tx.ExecContext(r.Context(), `UPDATE server_event_responses SET reminded=0 WHERE event_id=?`, eid)
		}
	}
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	v.ID = eid
	v.ServerID = sid
	v.CreatorID = uid
	s.emitServer(r.Context(), sid, "community.events", map[string]int64{"server_id": sid})
	return v, nil
}
func (s *Service) cancelEvent(r *http.Request, uid int64) (any, error) {
	sid, eid := path(r, "serverID"), path(r, "eventID")
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = eventManage(r.Context(), tx, sid, eid, uid); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE server_events SET cancelled=1,revision=revision+1 WHERE id=? AND server_id=?`, eid, sid); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	s.emitServer(r.Context(), sid, "community.events", map[string]int64{"server_id": sid})
	return map[string]bool{"ok": true}, nil
}
func (s *Service) eventResponse(r *http.Request, uid int64) (any, error) {
	sid, eid := path(r, "serverID"), path(r, "eventID")
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = serverMember(r.Context(), tx, sid, uid); err != nil {
		return nil, err
	}
	var valid bool
	if err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM server_events WHERE id=? AND server_id=? AND cancelled=0 AND starts_at>unixepoch())`, eid, sid).Scan(&valid); err != nil {
		return nil, err
	}
	if !valid {
		return nil, ErrConflict
	}
	if r.Method == "DELETE" {
		_, err = tx.ExecContext(r.Context(), `DELETE FROM server_event_responses WHERE event_id=? AND user_id=?`, eid, uid)
	} else {
		var v struct {
			Response string `json:"response"`
		}
		if err = decode(r, &v); err != nil {
			return nil, err
		}
		if v.Response != "going" && v.Response != "maybe" {
			return nil, ErrInvalid
		}
		_, err = tx.ExecContext(r.Context(), `INSERT INTO server_event_responses(event_id,user_id,response) VALUES(?,?,?) ON CONFLICT(event_id,user_id) DO UPDATE SET response=excluded.response`, eid, uid, v.Response)
	}
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	s.emitServer(r.Context(), sid, "community.events", map[string]int64{"server_id": sid})
	return map[string]bool{"ok": true}, nil
}
func (s *Service) processEventReminders(ctx context.Context, now int64) error {
	rows, err := s.db.QueryContext(ctx, `SELECT e.id,e.server_id,e.title,e.starts_at,r.user_id FROM server_events e JOIN server_event_responses r ON r.event_id=e.id JOIN member_permissions m ON m.server_id=e.server_id AND m.user_id=r.user_id AND (m.permissions&1)<>0 WHERE e.cancelled=0 AND e.starts_at>? AND e.starts_at<=? AND r.reminded=0 LIMIT 100`, now, now+900)
	if err != nil {
		return err
	}
	type notice struct {
		ID       int64  `json:"event_id"`
		ServerID int64  `json:"server_id"`
		Title    string `json:"title"`
		StartsAt int64  `json:"starts_at"`
		uid      int64
	}
	values := []notice{}
	for rows.Next() {
		var v notice
		if err = rows.Scan(&v.ID, &v.ServerID, &v.Title, &v.StartsAt, &v.uid); err != nil {
			break
		}
		values = append(values, v)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, v := range values {
		if _, err = s.db.ExecContext(ctx, `UPDATE server_event_responses SET reminded=1 WHERE event_id=? AND user_id=?`, v.ID, v.uid); err != nil {
			return err
		}
		s.emit(v.uid, "community.event-reminder", v)
	}
	return nil
}

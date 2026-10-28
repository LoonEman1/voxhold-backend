package community

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"
)

type Role struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Color       string  `json:"color"`
	Permissions int     `json:"permissions"`
	UserIDs     []int64 `json:"user_ids"`
}

var colorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func (s *Service) roles(r *http.Request, userID int64) (any, error) {
	serverID := path(r, "serverID")
	var member bool
	if err := s.db.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM server_members WHERE server_id=? AND user_id=?)`, serverID, userID).Scan(&member); err != nil {
		return nil, err
	}
	if !member {
		return nil, ErrForbidden
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT r.id,r.name,r.color,r.permissions,COALESCE((SELECT json_group_array(user_id) FROM member_roles a WHERE a.role_id=r.id),'[]') FROM server_roles r WHERE server_id=? ORDER BY id`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []Role{}
	for rows.Next() {
		v := Role{UserIDs: []int64{}}
		var ids string
		if err := rows.Scan(&v.ID, &v.Name, &v.Color, &v.Permissions, &ids); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(ids), &v.UserIDs); err != nil {
			return nil, err
		}
		values = append(values, v)
	}
	return values, rows.Err()
}
func (s *Service) saveRole(r *http.Request, userID int64) (any, error) {
	var input struct {
		Name        string `json:"name"`
		Color       string `json:"color"`
		Permissions int    `json:"permissions"`
	}
	if err := decode(r, &input); err != nil {
		return nil, err
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || utf8.RuneCountInString(input.Name) > 64 || input.Permissions < 0 || input.Permissions > 31 || !colorPattern.MatchString(input.Color) {
		return nil, ErrInvalid
	}
	serverID, id := path(r, "serverID"), path(r, "roleID")
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := owner(r.Context(), tx, serverID, userID); err != nil {
		return nil, err
	}
	var count int
	if err := tx.QueryRowContext(r.Context(), `SELECT count(*) FROM server_roles WHERE server_id=?`, serverID).Scan(&count); err != nil {
		return nil, err
	}
	if id == 0 && count >= 100 {
		return nil, ErrInvalid
	}
	var duplicate bool
	if err := tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM server_roles WHERE server_id=? AND name=? AND id<>?)`, serverID, input.Name, id).Scan(&duplicate); err != nil {
		return nil, err
	}
	if duplicate {
		return nil, ErrConflict
	}
	if id == 0 {
		err = tx.QueryRowContext(r.Context(), `INSERT INTO server_roles(server_id,name,color,permissions) VALUES(?,?,?,?) RETURNING id`, serverID, input.Name, input.Color, input.Permissions).Scan(&id)
	} else {
		err = tx.QueryRowContext(r.Context(), `UPDATE server_roles SET name=?,color=?,permissions=? WHERE id=? AND server_id=? RETURNING id`, input.Name, input.Color, input.Permissions, id, serverID).Scan(&id)
	}
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	s.rolesChanged(serverID)
	return Role{ID: id, Name: input.Name, Color: input.Color, Permissions: input.Permissions, UserIDs: []int64{}}, nil
}
func (s *Service) rolesChanged(serverID int64) {
	if s.changed != nil {
		s.changed(serverID)
	}
}
func (s *Service) deleteRole(r *http.Request, userID int64) (any, error) {
	serverID := path(r, "serverID")
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = owner(r.Context(), tx, serverID, userID); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(r.Context(), `DELETE FROM server_roles WHERE id=? AND server_id=?`, path(r, "roleID"), serverID); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	s.rolesChanged(serverID)
	return map[string]bool{"ok": true}, nil
}
func (s *Service) assignRoles(r *http.Request, userID int64) (any, error) {
	var input struct {
		RoleIDs []int64 `json:"role_ids"`
	}
	if err := decode(r, &input); err != nil {
		return nil, err
	}
	if len(input.RoleIDs) > 100 {
		return nil, ErrInvalid
	}
	serverID, target := path(r, "serverID"), path(r, "userID")
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = owner(r.Context(), tx, serverID, userID); err != nil {
		return nil, err
	}
	var role string
	if err = tx.QueryRowContext(r.Context(), `SELECT role FROM server_members WHERE server_id=? AND user_id=?`, serverID, target).Scan(&role); err != nil {
		return nil, err
	}
	if role == "owner" {
		return nil, ErrForbidden
	}
	if _, err = tx.ExecContext(r.Context(), `DELETE FROM member_roles WHERE server_id=? AND user_id=?`, serverID, target); err != nil {
		return nil, err
	}
	seen := map[int64]bool{}
	for _, id := range input.RoleIDs {
		if id <= 0 || seen[id] {
			return nil, ErrInvalid
		}
		seen[id] = true
		var exists bool
		if err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM server_roles WHERE server_id=? AND id=?)`, serverID, id).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, ErrInvalid
		}
		if _, err = tx.ExecContext(r.Context(), `INSERT INTO member_roles(server_id,user_id,role_id) VALUES(?,?,?)`, serverID, target, id); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	s.rolesChanged(serverID)
	return map[string]bool{"ok": true}, nil
}
func (s *Service) previewRole(r *http.Request, userID int64) (any, error) {
	serverID, id := path(r, "serverID"), path(r, "roleID")
	tx, err := s.db.BeginTx(r.Context(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = owner(r.Context(), tx, serverID, userID); err != nil {
		return nil, err
	}
	var bits int
	if err = tx.QueryRowContext(r.Context(), `SELECT permissions FROM server_roles WHERE id=? AND server_id=?`, id, serverID).Scan(&bits); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(r.Context(), `SELECT c.id,c.name,CASE WHEN c.is_news=1 THEN 'news' ELSE c.kind END FROM channels c WHERE c.server_id=? AND (? & 1)<>0 AND (c.access_restricted=0 OR EXISTS(SELECT 1 FROM channel_role_grants g WHERE g.channel_id=c.id AND g.role='member') OR EXISTS(SELECT 1 FROM channel_custom_role_grants g WHERE g.channel_id=c.id AND g.role_id=?)) ORDER BY c.position,c.id`, serverID, bits, id)
	if err != nil {
		return nil, err
	}
	values := []map[string]any{}
	for rows.Next() {
		var cid int64
		var name, kind string
		if err = rows.Scan(&cid, &name, &kind); err != nil {
			break
		}
		values = append(values, map[string]any{"id": cid, "name": name, "kind": kind, "permissions": bits, "can_publish": bits&2 != 0 && kind != "news"})
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	return values, err
}
func (s *Service) roleGrants(r *http.Request, userID int64) (any, error) {
	var input struct {
		RoleIDs []int64 `json:"role_ids"`
	}
	if r.Method != "GET" {
		if err := decode(r, &input); err != nil {
			return nil, err
		}
	}
	if len(input.RoleIDs) > 100 {
		return nil, ErrInvalid
	}
	serverID, cid := path(r, "serverID"), path(r, "channelID")
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = owner(r.Context(), tx, serverID, userID); err != nil {
		return nil, err
	}
	var exists bool
	if err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM channels WHERE id=? AND server_id=?)`, cid, serverID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrInvalid
	}
	if r.Method == "GET" {
		rows, err := tx.QueryContext(r.Context(), `SELECT role_id FROM channel_custom_role_grants WHERE channel_id=? ORDER BY role_id`, cid)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		input.RoleIDs = []int64{}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return nil, err
			}
			input.RoleIDs = append(input.RoleIDs, id)
		}
		return input, rows.Err()
	}
	if _, err = tx.ExecContext(r.Context(), `DELETE FROM channel_custom_role_grants WHERE channel_id=?`, cid); err != nil {
		return nil, err
	}
	for _, id := range input.RoleIDs {
		var valid bool
		if err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM server_roles WHERE id=? AND server_id=?)`, id, serverID).Scan(&valid); err != nil {
			return nil, err
		}
		if !valid {
			return nil, ErrInvalid
		}
		if _, err = tx.ExecContext(r.Context(), `INSERT INTO channel_custom_role_grants(channel_id,role_id) VALUES(?,?) ON CONFLICT DO NOTHING`, cid, id); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	s.rolesChanged(serverID)
	return input, nil
}

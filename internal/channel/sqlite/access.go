package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"voxhold-backend/internal/channel"
	"voxhold-backend/internal/server"
)

func requireManager(ctx context.Context, tx *sql.Tx, serverID, channelID, userID int64) error {
	var allowed bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM server_members WHERE server_id=c.server_id AND user_id=? AND role IN ('owner','admin')) FROM channels c WHERE c.server_id=? AND c.id=?`, userID, serverID, channelID).Scan(&allowed)
	if errors.Is(err, sql.ErrNoRows) {
		return channel.ErrNotFound
	}
	if err != nil {
		return err
	}
	if !allowed {
		return channel.ErrForbidden
	}
	return nil
}

func readAccess(ctx context.Context, tx *sql.Tx, channelID int64) (channel.Access, error) {
	value := channel.Access{UserIDs: []int64{}, Roles: []server.Role{}}
	if err := tx.QueryRowContext(ctx, `SELECT access_restricted FROM channels WHERE id=?`, channelID).Scan(&value.Restricted); err != nil {
		return value, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT user_id FROM channel_user_grants WHERE channel_id=? ORDER BY user_id`, channelID)
	if err != nil {
		return value, err
	}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			break
		}
		value.UserIDs = append(value.UserIDs, id)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return value, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT role FROM channel_role_grants WHERE channel_id=? ORDER BY role`, channelID)
	if err != nil {
		return value, err
	}
	for rows.Next() {
		var role server.Role
		if err = rows.Scan(&role); err != nil {
			break
		}
		value.Roles = append(value.Roles, role)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	return value, err
}

func (r *Repository) GetAccess(ctx context.Context, serverID, channelID, userID int64) (channel.Access, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return channel.Access{}, err
	}
	defer tx.Rollback()
	if err := requireManager(ctx, tx, serverID, channelID, userID); err != nil {
		return channel.Access{}, err
	}
	value, err := readAccess(ctx, tx, channelID)
	if err != nil {
		return value, err
	}
	return value, tx.Commit()
}

func (r *Repository) SetAccess(ctx context.Context, serverID, channelID, userID int64, value channel.Access) (channel.Access, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return channel.Access{}, err
	}
	defer tx.Rollback()
	if err := requireManager(ctx, tx, serverID, channelID, userID); err != nil {
		return channel.Access{}, err
	}
	if len(value.UserIDs) > 1000 {
		return channel.Access{}, channel.ErrAccessInvalid
	}
	if len(value.UserIDs) > 0 {
		args := make([]any, 0, len(value.UserIDs)+1)
		args = append(args, serverID)
		for _, id := range value.UserIDs {
			args = append(args, id)
		}
		var count int
		query := `SELECT COUNT(*) FROM server_members WHERE server_id=? AND user_id IN (` + strings.TrimSuffix(strings.Repeat("?,", len(value.UserIDs)), ",") + `)`
		if err := tx.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
			return channel.Access{}, err
		}
		if count != len(value.UserIDs) {
			return channel.Access{}, channel.ErrAccessInvalid
		}
	}

	if _, err := tx.ExecContext(ctx, `UPDATE channels SET access_restricted=? WHERE id=?`, value.Restricted, channelID); err != nil {
		return channel.Access{}, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM channel_user_grants WHERE channel_id=?`, channelID); err != nil {
		return channel.Access{}, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM channel_role_grants WHERE channel_id=?`, channelID); err != nil {
		return channel.Access{}, err
	}
	if len(value.UserIDs) > 0 {
		args := make([]any, 0, len(value.UserIDs)*3)
		for _, id := range value.UserIDs {
			args = append(args, channelID, serverID, id)
		}
		query := `INSERT INTO channel_user_grants(channel_id,server_id,user_id) VALUES` + strings.TrimSuffix(strings.Repeat("(?,?,?),", len(value.UserIDs)), ",")
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return channel.Access{}, err
		}
	}

	for _, role := range value.Roles {
		if _, err := tx.ExecContext(ctx, `INSERT INTO channel_role_grants(channel_id,role) VALUES(?,?)`, channelID, role); err != nil {
			return channel.Access{}, err
		}
	}
	result, err := readAccess(ctx, tx, channelID)
	if err != nil {
		return channel.Access{}, err
	}
	return result, tx.Commit()
}

// AccessibleChannelIDs is a snapshot refreshed on ACL/membership changes, not on
// media packets or every outgoing event. An error must fail closed in the hub.
func (r *Repository) AccessibleChannelIDs(userID int64) (map[int64]struct{}, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	rows, err := r.db.QueryContext(ctx, `SELECT channel_id FROM channel_accessible_members WHERE user_id=?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make(map[int64]struct{})
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = struct{}{}
	}
	return ids, rows.Err()
}

// AccessibleChannelIDsForUsers returns complete policies across all servers in
// one read transaction. Missing users get empty policies, never permissive nil.
func (r *Repository) AccessibleChannelIDsForUsers(userIDs []int64) (map[int64]map[int64]struct{}, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result := make(map[int64]map[int64]struct{}, len(userIDs))
	unique := make([]int64, 0, len(userIDs))
	for _, id := range userIDs {
		if _, ok := result[id]; !ok {
			result[id] = make(map[int64]struct{})
			unique = append(unique, id)
		}
	}
	if len(unique) == 0 {
		return result, nil
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for start := 0; start < len(unique); start += 500 {
		end := min(start+500, len(unique))
		args := make([]any, end-start)
		for i, id := range unique[start:end] {
			args[i] = id
		}
		rows, err := tx.QueryContext(ctx, `SELECT user_id,channel_id FROM channel_accessible_members WHERE user_id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(args)), ",")+`)`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var userID, channelID int64
			if err = rows.Scan(&userID, &channelID); err != nil {
				break
			}
			result[userID][channelID] = struct{}{}
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

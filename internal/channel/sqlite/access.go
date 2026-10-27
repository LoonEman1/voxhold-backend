package sqlite

import (
	"context"
	"database/sql"
	"errors"
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
	for _, id := range value.UserIDs {
		var member bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM server_members WHERE server_id=? AND user_id=?)`, serverID, id).Scan(&member); err != nil {
			return channel.Access{}, err
		}
		if !member {
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
	for _, id := range value.UserIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO channel_user_grants(channel_id,server_id,user_id) VALUES(?,?,?)`, channelID, serverID, id); err != nil {
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

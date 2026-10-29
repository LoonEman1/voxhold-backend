// Package audit records administrative changes in the same transaction as the mutation.
package audit

import (
	"context"
	"database/sql"
	"encoding/json"
)

func Record(ctx context.Context, tx *sql.Tx, serverID, actorID int64, action, targetType string, targetID int64, targetName string, details any) error {
	if targetName == "" && targetType == "user" {
		if err := tx.QueryRowContext(ctx, `SELECT username FROM users WHERE id=?`, targetID).Scan(&targetName); err != nil {
			return err
		}
	}
	data, err := json.Marshal(details)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO admin_audit(server_id,actor_user_id,actor_name,action,target_type,target_id,target_name,details) SELECT ?,?,username,?,?,?,?,? FROM users WHERE id=?`, serverID, actorID, action, targetType, targetID, targetName, string(data), actorID)
	return err
}

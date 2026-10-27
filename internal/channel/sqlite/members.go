package sqlite

import (
	"context"
	"database/sql"
)

func (r *Repository) ListMemberIDs(ctx context.Context, serverID, channelID, userID int64) ([]int64, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := checkAccess(ctx, tx, serverID, channelID, userID); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT user_id FROM channel_accessible_members WHERE server_id = ? AND channel_id = ? ORDER BY user_id`, serverID, channelID)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return ids, nil
}

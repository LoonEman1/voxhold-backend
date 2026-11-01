package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"voxhold-backend/internal/channel"
)

func (r *Repository) GetSlowmode(ctx context.Context, serverID, channelID, userID int64) (int, error) {
	var seconds int
	err := r.db.QueryRowContext(ctx, `SELECT c.slowmode_seconds FROM channels c
 JOIN server_members m ON m.server_id=c.server_id AND m.user_id=? WHERE c.id=? AND c.server_id=?`, userID, channelID, serverID).Scan(&seconds)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, channel.ErrNotFound
	}
	return seconds, err
}

func (r *Repository) SetSlowmode(ctx context.Context, serverID, channelID, userID int64, seconds int) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = requireManager(ctx, tx, serverID, channelID, userID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE channels SET slowmode_seconds=? WHERE id=? AND server_id=?`, seconds, channelID, serverID); err != nil {
		return err
	}
	return tx.Commit()
}

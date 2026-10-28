package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"voxhold-backend/internal/channel"
)

func readNews(ctx context.Context, tx *sql.Tx, channelID int64) (channel.NewsSettings, error) {
	value := channel.NewsSettings{PublisherIDs: []int64{}}
	var news bool
	if err := tx.QueryRowContext(ctx, `SELECT is_news,comments_enabled FROM channels WHERE id=?`, channelID).Scan(&news, &value.CommentsEnabled); err != nil {
		return value, err
	}
	if !news {
		return value, channel.ErrAccessInvalid
	}
	rows, err := tx.QueryContext(ctx, `SELECT user_id FROM channel_publishers WHERE channel_id=? ORDER BY user_id`, channelID)
	if err != nil {
		return value, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return value, err
		}
		value.PublisherIDs = append(value.PublisherIDs, id)
	}
	return value, rows.Err()
}

func (r *Repository) GetNews(ctx context.Context, serverID, channelID, userID int64) (channel.NewsSettings, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return channel.NewsSettings{}, err
	}
	defer tx.Rollback()
	if err := requireManager(ctx, tx, serverID, channelID, userID); err != nil {
		return channel.NewsSettings{}, err
	}
	value, err := readNews(ctx, tx, channelID)
	if err != nil {
		return value, err
	}
	return value, tx.Commit()
}

func (r *Repository) SetNews(ctx context.Context, serverID, channelID, userID int64, value channel.NewsSettings) (channel.NewsSettings, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return channel.NewsSettings{}, err
	}
	defer tx.Rollback()
	if err := requireManager(ctx, tx, serverID, channelID, userID); err != nil {
		return channel.NewsSettings{}, err
	}
	if _, err := readNews(ctx, tx, channelID); err != nil {
		return channel.NewsSettings{}, err
	}
	if len(value.PublisherIDs) > 1000 {
		return channel.NewsSettings{}, channel.ErrAccessInvalid
	}
	if value.PublisherIDs == nil {
		value.PublisherIDs = []int64{}
	}
	ids, err := json.Marshal(value.PublisherIDs)
	if err != nil {
		return channel.NewsSettings{}, err
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM server_members WHERE server_id=? AND user_id IN(SELECT value FROM json_each(?))`, serverID, string(ids)).Scan(&count); err != nil {
		return channel.NewsSettings{}, err
	}
	if count != len(value.PublisherIDs) {
		return channel.NewsSettings{}, channel.ErrAccessInvalid
	}
	if _, err := tx.ExecContext(ctx, `UPDATE channels SET comments_enabled=? WHERE id=?`, value.CommentsEnabled, channelID); err != nil {
		return channel.NewsSettings{}, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM channel_publishers WHERE channel_id=?`, channelID); err != nil {
		return channel.NewsSettings{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO channel_publishers(channel_id,server_id,user_id) SELECT ?,?,value FROM json_each(?)`, channelID, serverID, string(ids)); err != nil {
		return channel.NewsSettings{}, err
	}
	value, err = readNews(ctx, tx, channelID)
	if err != nil {
		return value, err
	}
	return value, tx.Commit()
}

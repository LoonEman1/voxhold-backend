package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"time"

	"voxhold-backend/internal/message"
)

const syncPageSize = 200

func (r *Repository) Sync(ctx context.Context, serverID, channelID, userID int64, after *int64) (message.SyncPage, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return message.SyncPage{}, err
	}
	defer tx.Rollback()
	if _, err := requireServerMember(ctx, tx, serverID, userID); err != nil {
		return message.SyncPage{}, err
	}
	if err := requireMessageChannel(ctx, tx, serverID, channelID, userID); err != nil {
		return message.SyncPage{}, err
	}
	page := message.SyncPage{Messages: []message.Message{}, DeletedIDs: []int64{}}
	var floor int64
	err = tx.QueryRowContext(ctx, `SELECT cursor, floor FROM message_sync_state WHERE channel_id = ?`, channelID).Scan(&page.Cursor, &floor)
	if err != nil && err != sql.ErrNoRows {
		return page, err
	}
	page.Reset = after == nil || *after < floor || *after > page.Cursor
	if page.Reset {
		rows, err := tx.QueryContext(ctx, `SELECT m.id, m.channel_id, m.author_user_id, u.username, m.content, m.created_at, m.edited_at, m.revision
			FROM messages m JOIN users u ON u.id = m.author_user_id WHERE m.channel_id = ? ORDER BY m.id DESC LIMIT 51`, channelID)
		if err != nil {
			return page, err
		}
		for rows.Next() {
			var value message.Message
			if err := rows.Scan(&value.ID, &value.ChannelID, &value.Author.UserID, &value.Author.Username, &value.Content, &value.CreatedAt, &value.EditedAt, &value.Revision); err != nil {
				rows.Close()
				return page, err
			}
			page.Messages = append(page.Messages, value)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return page, err
		}
		page.HasOlder = len(page.Messages) > 50
		if page.HasOlder {
			page.Messages = page.Messages[:50]
		}
		slices.Reverse(page.Messages)
	} else {
		rows, err := tx.QueryContext(ctx, `SELECT c.sequence, c.message_id, m.id, m.author_user_id, u.username, m.content, m.created_at, m.edited_at, m.revision
			FROM message_changes c LEFT JOIN messages m ON m.id = c.message_id LEFT JOIN users u ON u.id = m.author_user_id
			WHERE c.channel_id = ? AND c.sequence > ? ORDER BY c.sequence LIMIT ?`, channelID, *after, syncPageSize+1)
		if err != nil {
			return page, err
		}
		count := 0
		last := *after
		for rows.Next() {
			var sequence, messageID int64
			var id, authorID, createdAt, editedAt, revision sql.NullInt64
			var username, content sql.NullString
			if err := rows.Scan(&sequence, &messageID, &id, &authorID, &username, &content, &createdAt, &editedAt, &revision); err != nil {
				rows.Close()
				return page, err
			}
			if count == syncPageSize {
				page.HasMore = true
				break
			}
			count++
			last = sequence
			if !id.Valid {
				page.DeletedIDs = append(page.DeletedIDs, messageID)
				continue
			}
			value := message.Message{ID: id.Int64, ChannelID: channelID, Author: message.Author{UserID: authorID.Int64, Username: username.String}, Content: content.String, CreatedAt: createdAt.Int64, Revision: revision.Int64}
			if editedAt.Valid {
				value.EditedAt = &editedAt.Int64
			}
			page.Messages = append(page.Messages, value)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return page, err
		}
		if page.HasMore {
			page.Cursor = last
		}
	}
	if err := hydrateInteractions(ctx, tx, userID, messagePointers(page.Messages)); err != nil {
		return page, err
	}
	if err := tx.Commit(); err != nil {
		return page, err
	}
	return page, nil
}

// PruneChanges advances each affected channel's floor in the same transaction
// as deletion. Clients behind the floor must receive an authoritative snapshot.
func (r *Repository) PruneChanges(ctx context.Context) error {
	for batch := 0; batch < 200; batch++ {
		count, err := r.pruneChangeBatch(ctx)
		if err != nil {
			return err
		}
		if count < 500 {
			return nil
		}
	}
	return nil
}

func (r *Repository) pruneChangeBatch(ctx context.Context) (int, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT sequence, channel_id FROM message_changes
		WHERE created_at < ? OR sequence <= (SELECT COALESCE(MAX(sequence), 0) - 100000 FROM message_changes)
		ORDER BY sequence LIMIT 500`, time.Now().Add(-24*time.Hour).Unix())
	if err != nil {
		return 0, fmt.Errorf("select expired message journal: %w", err)
	}
	floors := make(map[int64]int64)
	count := 0
	for rows.Next() {
		var sequence, channelID int64
		if err := rows.Scan(&sequence, &channelID); err != nil {
			rows.Close()
			return 0, err
		}
		floors[channelID] = max(floors[channelID], sequence)
		count++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	for channelID, floor := range floors {
		if _, err := tx.ExecContext(ctx, `UPDATE message_sync_state SET floor = MAX(floor, ?) WHERE channel_id = ?`, floor, channelID); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM message_changes WHERE channel_id = ? AND sequence <= ?`, channelID, floor); err != nil {
			return 0, err
		}
	}
	return count, tx.Commit()
}

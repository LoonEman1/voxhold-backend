package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"

	"voxhold-backend/internal/mentions"
	"voxhold-backend/internal/message"
)

func requestHash(input message.CreateInput, forward *message.ForwardInput) (string, string) {
	kind, source := "message", []byte(input.Content)
	if forward != nil {
		kind = "forward"
		source, _ = json.Marshal([]int64{forward.SourceServerID, forward.SourceChannelID, forward.SourceMessageID})
	} else if input.ReplyToMessageID != nil {
		kind = "reply"
		source, _ = json.Marshal([]any{input.Content, *input.ReplyToMessageID})
	}
	if input.ThreadRootID != nil {
		kind = "reply"
		source, _ = json.Marshal(map[string]any{"type": "thread", "content": input.Content, "reply": input.ReplyToMessageID, "root": *input.ThreadRootID})
	}
	if len(input.AttachmentIDs) > 0 {
		kind = "reply"
		source, _ = json.Marshal(map[string]any{"type": "attachment", "content": input.Content, "reply": input.ReplyToMessageID, "root": input.ThreadRootID, "ids": input.AttachmentIDs})
	}
	return kind, fmt.Sprintf("%x", sha256.Sum256(source))
}

func forwardSnapshot(ctx context.Context, tx *sql.Tx, userID int64, input message.ForwardInput) (string, *message.ForwardReference, error) {
	if err := requireInteraction(ctx, tx, input.SourceServerID, input.SourceChannelID, input.SourceMessageID, userID); err != nil {
		return "", nil, err
	}
	value, err := findMessage(ctx, tx, input.SourceChannelID, input.SourceMessageID)
	if err != nil {
		return "", nil, err
	}
	var stored sql.NullString
	if err = tx.QueryRowContext(ctx, `SELECT forward_data FROM messages WHERE id=?`, value.ID).Scan(&stored); err != nil {
		return "", nil, err
	}
	if stored.Valid {
		var snapshot message.ForwardReference
		if err = json.Unmarshal([]byte(stored.String), &snapshot); err != nil {
			return "", nil, err
		}
		return value.Content, &snapshot, nil
	}
	snapshot := &message.ForwardReference{Author: value.Author, CreatedAt: value.CreatedAt, MentionNames: map[int64]string{}}
	targets := mentions.Parse(value.Content)
	ids := make([]int64, 0, len(targets.Users))
	for id := range targets.Users {
		ids = append(ids, id)
	}
	if len(ids) > 0 {
		encoded, _ := json.Marshal(ids)
		rows, err := tx.QueryContext(ctx, `SELECT u.id,u.username FROM users u JOIN server_members m ON m.user_id=u.id
 WHERE m.server_id=? AND u.id IN (SELECT value FROM json_each(?))`, input.SourceServerID, string(encoded))
		if err != nil {
			return "", nil, err
		}
		for rows.Next() {
			var id int64
			var name string
			if err = rows.Scan(&id, &name); err != nil {
				break
			}
			snapshot.MentionNames[id] = name
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return "", nil, err
		}
	}
	return value.Content, snapshot, nil
}

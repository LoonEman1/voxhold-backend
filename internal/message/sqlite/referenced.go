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
		if forward.SourceConversationID > 0 {
			source, _ = json.Marshal([]any{"conversation", forward.SourceConversationID, forward.SourceMessageID})
		} else {
			source, _ = json.Marshal([]int64{forward.SourceServerID, forward.SourceChannelID, forward.SourceMessageID})
		}
		if len(input.AttachmentIDs) > 0 {
			source, _ = json.Marshal([]any{string(source), input.AttachmentIDs})
		}
	} else if input.ReplyToMessageID != nil {
		kind = "reply"
		source, _ = json.Marshal([]any{input.Content, *input.ReplyToMessageID})
	}
	if input.ThreadRootID != nil {
		kind = "reply"
		source, _ = json.Marshal(map[string]any{"type": "thread", "content": input.Content, "reply": input.ReplyToMessageID, "root": *input.ThreadRootID})
	}
	if len(input.AttachmentIDs) > 0 && forward == nil {
		kind = "reply"
		source, _ = json.Marshal(map[string]any{"type": "attachment", "content": input.Content, "reply": input.ReplyToMessageID, "root": input.ThreadRootID, "ids": input.AttachmentIDs})
	}
	if input.Poll != nil {
		kind = "reply"
		source, _ = json.Marshal([]any{string(source), input.Poll})
	}
	return kind, fmt.Sprintf("%x", sha256.Sum256(source))
}

func forwardSnapshot(ctx context.Context, tx *sql.Tx, userID int64, input message.ForwardInput) (string, *message.ForwardReference, error) {
	if input.SourceConversationID > 0 {
		var content string
		var snapshot message.ForwardReference
		var stored sql.NullString
		err := tx.QueryRowContext(ctx, `SELECT m.content,m.author_user_id,u.username,m.created_at,m.forward_data FROM conversation_messages m JOIN users u ON u.id=m.author_user_id WHERE m.id=? AND m.conversation_id=? AND EXISTS(SELECT 1 FROM conversation_members WHERE conversation_id=m.conversation_id AND user_id=?)`, input.SourceMessageID, input.SourceConversationID, userID).Scan(&content, &snapshot.Author.UserID, &snapshot.Author.Username, &snapshot.CreatedAt, &stored)
		if err == sql.ErrNoRows {
			return "", nil, message.ErrForbidden
		}
		if err != nil {
			return "", nil, err
		}
		if stored.Valid {
			if err = json.Unmarshal([]byte(stored.String), &snapshot); err != nil {
				return "", nil, err
			}
		}
		return content, &snapshot, nil
	}
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

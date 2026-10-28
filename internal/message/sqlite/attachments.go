package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"voxhold-backend/internal/message"
)

func hydrateAttachments(ctx context.Context, tx *sql.Tx, values []*message.Message) error {
	ids := []int64{}
	byID := map[int64][]*message.Message{}
	for _, v := range values {
		ids = append(ids, v.ID)
		byID[v.ID] = append(byID[v.ID], v)
		v.Attachments = []message.Attachment{}
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT message_id,id,filename,mime_type,size_bytes,deleted_at IS NOT NULL FROM attachments WHERE message_id IN (SELECT value FROM json_each(?)) ORDER BY id`, string(encoded))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var a message.Attachment
		if err := rows.Scan(&id, &a.ID, &a.Filename, &a.MIMEType, &a.SizeBytes, &a.Deleted); err != nil {
			return err
		}
		for _, v := range byID[id] {
			v.Attachments = append(v.Attachments, a)
		}
	}
	return rows.Err()
}

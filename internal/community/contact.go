package community

import (
	"context"
	"database/sql"
)

// CanSend checks every recipient, including opt-outs changed after creation.
func CanSend(ctx context.Context, db *sql.DB, id, userID int64) error {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := conversationAccess(ctx, tx, id, userID); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT user_id FROM conversation_members WHERE conversation_id=? AND user_id<>?`, id, userID)
	if err != nil {
		return err
	}
	ids := []int64{}
	for rows.Next() {
		var uid int64
		if err = rows.Scan(&uid); err != nil {
			break
		}
		ids = append(ids, uid)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return ErrForbidden
	}
	for _, uid := range ids {
		if err := canContact(ctx, tx, userID, uid); err != nil {
			return err
		}
	}
	return tx.Commit()
}

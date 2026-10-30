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
	if err := canSendTransaction(ctx, tx, id, userID); err != nil {
		return err
	}
	return tx.Commit()
}
func canSendTransaction(ctx context.Context, tx *sql.Tx, id, userID int64) error {
	if err := activeActor(ctx, tx, userID); err != nil {
		return err
	}
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
	var saved bool
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(saved_owner_id=?,0) FROM conversations WHERE id=?`, userID, id).Scan(&saved); err != nil {
		return err
	}
	if len(ids) == 0 && !saved {
		return ErrForbidden
	}
	for _, uid := range ids {
		if err := canContact(ctx, tx, userID, uid); err != nil {
			return err
		}
	}
	return nil
}

// Background operations must enforce account revocation inside their transaction.
func activeActor(ctx context.Context, q queryer, userID int64) error {
	var active bool
	err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users u WHERE u.id=? AND u.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM user_bans b WHERE b.user_id=u.id))`, userID).Scan(&active)
	if err != nil {
		return err
	}
	if !active {
		return ErrForbidden
	}
	return nil
}

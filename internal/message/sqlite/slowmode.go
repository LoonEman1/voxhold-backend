package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// CheckSlowmode returns how long a plain member must still wait in a channel
// with slowmode enabled; owners and admins are never delayed.
func (r *Repository) CheckSlowmode(ctx context.Context, serverID, channelID, userID int64) (time.Duration, error) {
	return checkSlowmode(ctx, r.db, serverID, channelID, userID)
}

type slowmodeQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// The send path calls this with its write transaction, after checking the
// idempotency key, so concurrent sends cannot bypass the interval and retries
// still return the message that was already committed.
func checkSlowmode(ctx context.Context, q slowmodeQuerier, serverID, channelID, userID int64) (time.Duration, error) {
	var seconds int
	var role string
	err := q.QueryRowContext(ctx, `SELECT c.slowmode_seconds, COALESCE(m.role,'') FROM channels c
 LEFT JOIN server_members m ON m.server_id=c.server_id AND m.user_id=? WHERE c.id=? AND c.server_id=?`, userID, channelID, serverID).Scan(&seconds, &role)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if seconds == 0 || role == "owner" || role == "admin" {
		return 0, nil
	}
	var last int64
	err = q.QueryRowContext(ctx, `SELECT created_at FROM messages WHERE channel_id=? AND author_user_id=? ORDER BY id DESC LIMIT 1`, channelID, userID).Scan(&last)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	wait := time.Until(time.Unix(last, 0).Add(time.Duration(seconds) * time.Second))
	if wait < 0 {
		return 0, nil
	}
	return wait, nil
}

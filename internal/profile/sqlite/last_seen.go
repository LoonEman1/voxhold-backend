package sqlite

import "context"

// RecordLastSeen is monotonic: delayed disconnect/connect writes cannot move
// the durable timestamp backwards. A deleted account is never recreated.
func (r *Repository) RecordLastSeen(ctx context.Context, userID, at int64) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO user_profiles(user_id, last_seen_at)
		SELECT id, ? FROM users WHERE id = ?
		ON CONFLICT(user_id) DO UPDATE SET last_seen_at = excluded.last_seen_at
		WHERE excluded.last_seen_at > COALESCE(user_profiles.last_seen_at, -1)`, at, userID)
	return err
}

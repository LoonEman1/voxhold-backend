package community

import "context"

// RevokeInvalidCallDevices removes only encrypted call leases whose device
// has been revoked (explicitly or by LRU registration). Remaining members get
// a new call state and rotate their sender keys.
func (s *Service) RevokeInvalidCallDevices(ctx context.Context, userID int64) error {
	rows, err := s.db.QueryContext(ctx, `DELETE FROM conversation_call_members AS m
 WHERE m.user_id=? AND m.e2ee_device_id IS NOT NULL
 AND NOT EXISTS(SELECT 1 FROM e2ee_device_keys k WHERE k.user_id=m.user_id AND k.device_id=m.e2ee_device_id AND k.revoked_at IS NULL)
 RETURNING conversation_id`, userID)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	if closeErr := rows.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	for _, id := range ids {
		state, err := s.readCall(ctx, id)
		if err != nil {
			return err
		}
		s.emitConversation(ctx, id, "conversation.call", state)
	}
	return nil
}

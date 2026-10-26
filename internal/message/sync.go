package message

import (
	"context"
	"errors"
)

var (
	ErrClientMessageIDInvalid = errors.New("invalid client_message_id")
	ErrIdempotencyConflict    = errors.New("client_message_id was already used for different content")
)

type SyncPage struct {
	Messages   []Message
	DeletedIDs []int64
	Cursor     int64
	Reset      bool
	HasMore    bool
	HasOlder   bool
}

// Sync reads both the cursor and the messages from one database snapshot.
func (s *Service) Sync(ctx context.Context, serverID, channelID, userID int64, after *int64) (SyncPage, error) {
	if serverID <= 0 || channelID <= 0 {
		return SyncPage{}, ErrChannelNotFound
	}
	if userID <= 0 {
		return SyncPage{}, ErrForbidden
	}
	return s.repository.Sync(ctx, serverID, channelID, userID, after)
}

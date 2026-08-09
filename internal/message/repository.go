package message

import "context"

type Repository interface {
	Create(
		ctx context.Context,
		serverID int64,
		channelID int64,
		authorUserID int64,
		content string,
	) (Message, error)

	ListByChannelID(
		ctx context.Context,
		serverID int64,
		channelID int64,
		userID int64,
		beforeID *int64,
		limit int,
	) ([]Message, error)
}

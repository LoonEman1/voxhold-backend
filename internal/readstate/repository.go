package readstate

import "context"

type Repository interface {
	Mark(
		ctx context.Context,
		serverID int64,
		channelID int64,
		userID int64,
		lastReadMessageID int64,
	) (ChannelRead, bool, error)

	ListByUserID(
		ctx context.Context,
		userID int64,
	) ([]ChannelRead, error)

	ListByChannelID(
		ctx context.Context,
		serverID int64,
		channelID int64,
		requesterUserID int64,
	) ([]ChannelRead, error)
}

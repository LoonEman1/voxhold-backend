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

	Update(
		ctx context.Context,
		serverID int64,
		channelID int64,
		messageID int64,
		userID int64,
		content string,
	) (Message, error)

	Delete(
		ctx context.Context,
		serverID int64,
		channelID int64,
		messageID int64,
		userID int64,
	) (Message, error)

	Pin(
		ctx context.Context,
		serverID int64,
		channelID int64,
		messageID int64,
		userID int64,
	) (Pin, bool, error)

	Unpin(
		ctx context.Context,
		serverID int64,
		channelID int64,
		messageID int64,
		userID int64,
	) (bool, error)

	ListPinned(
		ctx context.Context,
		serverID int64,
		channelID int64,
		userID int64,
	) ([]PinnedMessage, error)

	Search(
		ctx context.Context,
		serverID int64,
		userID int64,
		query string,
		beforeID *int64,
		limit int,
	) ([]SearchResult, error)

	GetContext(
		ctx context.Context,
		serverID int64,
		channelID int64,
		messageID int64,
		userID int64,
		before int,
		after int,
	) (Context, error)
}

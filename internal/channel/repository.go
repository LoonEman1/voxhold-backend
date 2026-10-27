package channel

import "context"

type Repository interface {
	AccessRepository
	ListMemberIDs(ctx context.Context, serverID, channelID, userID int64) ([]int64, error)

	Create(
		ctx context.Context,
		serverID int64,
		userID int64,
		name string,
		kind Kind,
	) (Channel, error)

	ListByServerID(
		ctx context.Context,
		serverID int64,
		userID int64,
	) ([]Channel, error)

	Update(
		ctx context.Context,
		serverID int64,
		channelID int64,
		userID int64,
		name string,
	) (Channel, error)

	Delete(
		ctx context.Context,
		serverID int64,
		channelID int64,
		userID int64,
	) (Channel, error)

	CheckAccess(
		ctx context.Context,
		serverID int64,
		channelID int64,
		userID int64,
	) error

	CheckVoiceAccess(
		ctx context.Context,
		serverID int64,
		channelID int64,
		userID int64,
	) error
}

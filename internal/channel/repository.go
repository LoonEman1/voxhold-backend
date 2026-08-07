package channel

import "context"

type Repository interface {
	Create(
		ctx context.Context,
		serverID int64,
		userID int64,
		name string,
		kind Kind,
	) (Channel, error)
}

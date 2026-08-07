package invite

import "context"

type Repository interface {
	CreateDirect(
		ctx context.Context,
		serverID int64,
		inviterUserID int64,
		inviteeUsername string,
		expiresAt int64,
	) (Invite, error)
}

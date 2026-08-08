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

	ListIncoming(
		ctx context.Context,
		inviteeUserID int64,
	) ([]IncomingInvite, error)

	Accept(
		ctx context.Context,
		inviteID int64,
		inviteeUserID int64,
	) error

	Decline(
		ctx context.Context,
		inviteID int64,
		inviteeUserID int64,
	) error
}

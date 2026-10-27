package invite

import (
	"context"

	"voxhold-backend/internal/server"
)

type Repository interface {
	ListActiveLinks(ctx context.Context, serverID, userID int64, beforeID *int64, limit int) ([]InviteLink, error)
	RevokeLink(ctx context.Context, serverID, linkID, userID int64) error
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
	) (server.JoinedServer, server.ServerMember, error)

	Decline(
		ctx context.Context,
		inviteID int64,
		inviteeUserID int64,
	) error

	CreateLink(
		ctx context.Context,
		serverID int64,
		creatorUserID int64,
		tokenHash []byte,
		expiresAt int64,
		maxUses *int,
		allowRegistration bool,
	) (InviteLink, error)

	ResolveLink(
		ctx context.Context,
		tokenHash []byte,
	) (LinkPreview, error)

	AcceptLink(
		ctx context.Context,
		tokenHash []byte,
		userID int64,
	) (server.JoinedServer, server.ServerMember, bool, error)
}

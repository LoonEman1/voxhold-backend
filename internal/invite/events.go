package invite

import "voxhold-backend/internal/server"

type MembershipRegistrar interface {
	AddUserToServer(
		userID int64,
		serverID int64,
	)
}

type EventPublisher interface {
	PublishInvitationReceived(invitation Invite)
}

type MemberEventPublisher interface {
	PublishServerMemberJoined(
		serverID int64,
		member server.ServerMember,
	)
}

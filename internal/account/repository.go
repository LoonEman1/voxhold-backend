package account

import (
	"context"

	"voxhold-backend/internal/server"
)

type UserRepository interface {
	Create(
		ctx context.Context,
		username string,
		passwordHash string,
	) (UserInfo, error)

	FindByUsername(
		ctx context.Context,
		username string,
	) (User, error)
}

type SessionRepository interface {
	Create(
		ctx context.Context,
		userID int64,
		tokenHash []byte,
		expiresAt int64,
	) error

	DeleteByTokenHash(
		ctx context.Context,
		tokenHash []byte,
	) error

	FindActiveUserIDByTokenHash(
		ctx context.Context,
		tokenHash []byte,
	) (int64, error)

	Rotate(
		ctx context.Context,
		oldTokenHash []byte,
		newTokenHash []byte,
		newExpiresAt int64,
	) error
}

type InviteRegistrationRepository interface {
	ValidateRegistrationInvite(
		ctx context.Context,
		inviteTokenHash []byte,
	) error

	RegisterWithInvite(
		ctx context.Context,
		username string,
		passwordHash string,
		inviteTokenHash []byte,
		sessionTokenHash []byte,
		sessionExpiresAt int64,
	) (UserInfo, int64, server.ServerMember, error)
}

type MembershipRegistrar interface {
	AddUserToServer(userID int64, serverID int64)
}

type MemberEventPublisher interface {
	PublishServerMemberJoined(serverID int64, member server.ServerMember)
}

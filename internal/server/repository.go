package server

import "context"

type Repository interface {
	GetInstance(ctx context.Context) (Instance, error)

	Create(
		ctx context.Context,
		name string,
		createdBy int64,
	) (Server, error)
	Update(
		ctx context.Context,
		serverID int64,
		userID int64,
		name string,
	) (Server, error)
	Delete(
		ctx context.Context,
		serverID int64,
		userID int64,
	) error

	DeleteAccount(
		ctx context.Context,
		userID int64,
	) (int64, error)

	ListByUserID(
		ctx context.Context,
		userID int64,
	) ([]JoinedServer, error)

	ListMembers(
		ctx context.Context,
		serverID int64,
		requesterUserID int64,
	) ([]ServerMember, error)

	UpdateMemberRole(
		ctx context.Context,
		serverID int64,
		requesterUserID int64,
		targetUserID int64,
		role Role,
	) (ServerMember, bool, error)

	BanMember(
		ctx context.Context,
		serverID int64,
		requesterUserID int64,
		targetUserID int64,
	) error
}

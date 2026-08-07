package server

import "context"

type Repository interface {
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
}

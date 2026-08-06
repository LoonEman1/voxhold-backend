package server

import "context"

type Repository interface {
	Create(
		ctx context.Context,
		name string,
		createdBy int64,
	) (Server, error)
}

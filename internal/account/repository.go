package account

import "context"

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
}

package profile

import "context"

type Repository interface {
	GetByUserID(
		ctx context.Context,
		userID int64,
	) (Profile, error)

	Update(
		ctx context.Context,
		userID int64,
		about *string,
		countryCode *string,
	) (Profile, error)

	GetVisibleByUserID(
		ctx context.Context,
		requesterUserID int64,
		targetUserID int64,
	) (Profile, error)
}

package account

import "context"

type contextKey string

const userIDContextKey contextKey = "authenticated_user_id"

func ContextWithUserID(
	ctx context.Context,
	userID int64,
) context.Context {
	return context.WithValue(
		ctx,
		userIDContextKey,
		userID,
	)
}

func UserIDFromContext(
	ctx context.Context,
) (int64, bool) {
	userID, ok := ctx.Value(userIDContextKey).(int64)

	return userID, ok
}

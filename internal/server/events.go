package server

type AccessRevoker interface {
	RevokeUserFromServer(
		userID int64,
		serverID int64,
	)

	RevokeServer(serverID int64)
}

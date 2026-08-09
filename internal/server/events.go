package server

type AccessRevoker interface {
	AddUserToServer(
		userID int64,
		serverID int64,
	)

	RevokeUserFromServer(
		userID int64,
		serverID int64,
	)

	RevokeServer(serverID int64)
}

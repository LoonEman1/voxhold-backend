package invite

type MembershipRegistrar interface {
	AddUserToServer(
		userID int64,
		serverID int64,
	)
}

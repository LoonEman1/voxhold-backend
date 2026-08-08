package invite

type IncomingInvite struct {
	ID              int64
	ServerID        int64
	ServerName      string
	InviterUserID   int64
	InviterUsername string
	Status          Status
	ExpiresAt       int64
	CreatedAt       int64
}

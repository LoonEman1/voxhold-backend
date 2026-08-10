package invite

type Status string

const (
	StatusPending  Status = "pending"
	StatusAccepted Status = "accepted"
	StatusDeclined Status = "declined"
	StatusCanceled Status = "canceled"
	StatusExpired  Status = "expired"
)

type Invite struct {
	ID              int64
	ServerID        int64
	ServerName      string
	InviterUserID   int64
	InviterUsername string
	InviteeUserID   int64
	Status          Status
	ExpiresAt       int64
	RespondedAt     *int64
	CreatedAt       int64
}

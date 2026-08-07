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
	ID            int64
	ServerID      int64
	InviterUserID int64
	InviteeUserID int64
	Status        Status
	ExpiresAt     int64
	RespondedAt   *int64
	CreatedAt     int64
}

package channel

type Kind string

const (
	KindText  Kind = "text"
	KindVoice Kind = "voice"
	KindNews  Kind = "news"
)

func (k Kind) IsValid() bool {
	switch k {
	case KindText, KindVoice, KindNews:
		return true
	default:
		return false
	}
}

type Channel struct {
	ID               int64
	ServerID         int64
	Name             string
	Kind             Kind
	Position         int64
	CreatedBy        int64
	CreatedAt        int64
	AccessRestricted bool
	LastMessageID    int64
	UnreadCount      int64
	CommentsEnabled  bool
	CanPublish       bool
}

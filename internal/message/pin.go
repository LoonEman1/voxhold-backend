package message

type PinnedMessage struct {
	Message  Message
	PinnedBy Author
	PinnedAt int64
}

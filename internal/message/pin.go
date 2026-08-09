package message

type Pin struct {
	MessageID int64
	ChannelID int64
	PinnedBy  Author
	PinnedAt  int64
}

type PinnedMessage struct {
	Message  Message
	PinnedBy Author
	PinnedAt int64
}

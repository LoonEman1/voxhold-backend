package realtime

const (
	EventMessagePinned   EventType = "message.pinned"
	EventMessageUnpinned EventType = "message.unpinned"
)

type MessageDeletedData struct {
	ChannelID int64 `json:"channel_id"`
	MessageID int64 `json:"message_id"`
}

type MessagePinnedData struct {
	ChannelID int64              `json:"channel_id"`
	MessageID int64              `json:"message_id"`
	Message   MessageCreatedData `json:"message"`
	PinnedBy  MessageAuthorData  `json:"pinned_by"`
	PinnedAt  int64              `json:"pinned_at"`
}

type MessageUnpinnedData struct {
	ChannelID int64 `json:"channel_id"`
	MessageID int64 `json:"message_id"`
}

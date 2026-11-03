package realtime

import "voxhold-backend/internal/message"

var _ message.EventPublisher = (*MessageEventPublisher)(nil)

type MessageEventPublisher struct {
	hub       *Hub
	onCreated func(message.Message)
}

// SetCreatedListener is configured before accepting requests.
func (p *MessageEventPublisher) SetCreatedListener(listener func(message.Message)) {
	p.onCreated = listener
}

func NewMessageEventPublisher(
	hub *Hub,
) *MessageEventPublisher {
	return &MessageEventPublisher{
		hub: hub,
	}
}

func (p *MessageEventPublisher) PublishMessageCreated(
	value message.Message,
) {
	if value.Replayed {
		return
	}
	p.hub.Publish(
		value.ChannelID,
		OutgoingEvent{
			Type: EventMessageCreated,
			Data: MessageCreatedData{
				Attachments:  value.Attachments,
				Poll:         value.Poll,
				ThreadRootID: value.ThreadRootID, CommentCount: value.CommentCount,
				ClientMessageID: value.ClientMessageID,
				ID:              value.ID,
				ChannelID:       value.ChannelID,
				Author: MessageAuthorData{
					UserID:   value.Author.UserID,
					Username: value.Author.Username,
				},
				Content:   value.Content,
				CreatedAt: value.CreatedAt,
				EditedAt:  value.EditedAt,
				Revision:  value.Revision,
				Reply:     value.Reply, Forward: value.Forward,
			},
		},
	)
	if !value.Replayed && p.onCreated != nil {
		p.onCreated(value)
	}
}

func (p *MessageEventPublisher) PublishMessageUpdated(
	value message.Message,
) {
	p.hub.Publish(
		value.ChannelID,
		OutgoingEvent{
			Type: EventMessageUpdated,
			Data: newMessageData(value),
		},
	)
}

func (p *MessageEventPublisher) PublishMessageDeleted(
	value message.Message,
) {
	p.hub.Publish(
		value.ChannelID,
		OutgoingEvent{
			Type: EventMessageDeleted,
			Data: MessageDeletedData{
				ChannelID: value.ChannelID,
				MessageID: value.ID,
			},
		},
	)
}

func (p *MessageEventPublisher) PublishMessagePinned(
	value message.PinnedMessage,
) {
	p.hub.Publish(
		value.Message.ChannelID,
		OutgoingEvent{
			Type: EventMessagePinned,
			Data: MessagePinnedData{
				ChannelID: value.Message.ChannelID,
				MessageID: value.Message.ID,
				Message:   newMessageData(value.Message),
				PinnedBy: MessageAuthorData{
					UserID:   value.PinnedBy.UserID,
					Username: value.PinnedBy.Username,
				},
				PinnedAt: value.PinnedAt,
			},
		},
	)
}

func (p *MessageEventPublisher) PublishMessageUnpinned(
	channelID int64,
	messageID int64,
) {
	p.hub.Publish(
		channelID,
		OutgoingEvent{
			Type: EventMessageUnpinned,
			Data: MessageUnpinnedData{
				ChannelID: channelID,
				MessageID: messageID,
			},
		},
	)
}

func newMessageData(
	value message.Message,
) MessageCreatedData {
	return MessageCreatedData{
		Attachments:  value.Attachments,
		Poll:         value.Poll,
		ThreadRootID: value.ThreadRootID, CommentCount: value.CommentCount,
		ID:        value.ID,
		ChannelID: value.ChannelID,
		Author: MessageAuthorData{
			UserID:   value.Author.UserID,
			Username: value.Author.Username,
		},
		Content:   value.Content,
		CreatedAt: value.CreatedAt,
		EditedAt:  value.EditedAt,
		Revision:  value.Revision,
		Reply:     value.Reply, Forward: value.Forward,
	}
}

// EventChannelActivity tells channel members (not only subscribers) that a
// channel has a new top-level message. It carries no content.
const EventChannelActivity EventType = "channel.activity"

type ChannelActivityData struct {
	ServerID     int64 `json:"server_id"`
	ChannelID    int64 `json:"channel_id"`
	MessageID    int64 `json:"message_id"`
	AuthorUserID int64 `json:"author_user_id"`
}

// EventChannelID limits the event to clients that can read the channel.
func (d ChannelActivityData) EventChannelID() int64 { return d.ChannelID }

func (p *MessageEventPublisher) PublishChannelActivity(serverID int64, value message.Message) {
	p.hub.PublishToServer(serverID, OutgoingEvent{
		Type: EventChannelActivity,
		Data: ChannelActivityData{ServerID: serverID, ChannelID: value.ChannelID, MessageID: value.ID, AuthorUserID: value.Author.UserID},
	})
}

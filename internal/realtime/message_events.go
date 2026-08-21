package realtime

import "voxhold-backend/internal/message"

var _ message.EventPublisher = (*MessageEventPublisher)(nil)

type MessageEventPublisher struct {
	hub *Hub
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
	p.hub.Publish(
		value.ChannelID,
		OutgoingEvent{
			Type: EventMessageCreated,
			Data: MessageCreatedData{
				ID:        value.ID,
				ChannelID: value.ChannelID,
				Author: MessageAuthorData{
					UserID:   value.Author.UserID,
					Username: value.Author.Username,
				},
				Content:   value.Content,
				CreatedAt: value.CreatedAt,
				EditedAt:  value.EditedAt,
			},
		},
	)
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
		ID:        value.ID,
		ChannelID: value.ChannelID,
		Author: MessageAuthorData{
			UserID:   value.Author.UserID,
			Username: value.Author.Username,
		},
		Content:   value.Content,
		CreatedAt: value.CreatedAt,
		EditedAt:  value.EditedAt,
	}
}

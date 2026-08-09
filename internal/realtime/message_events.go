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

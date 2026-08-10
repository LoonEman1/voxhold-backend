package realtime

import "voxhold-backend/internal/channel"

var _ channel.EventPublisher = (*ChannelEventPublisher)(nil)

type ChannelEventPublisher struct {
	hub *Hub
}

func NewChannelEventPublisher(
	hub *Hub,
) *ChannelEventPublisher {
	return &ChannelEventPublisher{hub: hub}
}

func (p *ChannelEventPublisher) PublishChannelCreated(
	value channel.Channel,
) {
	p.publishChannel(
		EventChannelCreated,
		value,
	)
}

func (p *ChannelEventPublisher) PublishChannelUpdated(
	value channel.Channel,
) {
	p.publishChannel(
		EventChannelUpdated,
		value,
	)
}

func (p *ChannelEventPublisher) PublishChannelDeleted(
	value channel.Channel,
) {
	p.hub.PublishToServer(
		value.ServerID,
		OutgoingEvent{
			Type: EventChannelDeleted,
			Data: ChannelDeletedData{
				ServerID:  value.ServerID,
				ChannelID: value.ID,
			},
		},
	)

	p.hub.RemoveChannel(value.ID)
}

func (p *ChannelEventPublisher) publishChannel(
	eventType EventType,
	value channel.Channel,
) {
	p.hub.PublishToServer(
		value.ServerID,
		OutgoingEvent{
			Type: eventType,
			Data: ChannelData{
				ID:        value.ID,
				ServerID:  value.ServerID,
				Name:      value.Name,
				Kind:      string(value.Kind),
				Position:  value.Position,
				CreatedBy: value.CreatedBy,
				CreatedAt: value.CreatedAt,
			},
		},
	)
}

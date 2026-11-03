package message

type EventPublisher interface {
	PublishMessageCreated(
		value Message,
	)

	PublishMessageUpdated(
		value Message,
	)

	PublishMessageDeleted(
		value Message,
	)

	PublishMessagePinned(value PinnedMessage)

	PublishMessageUnpinned(
		channelID int64,
		messageID int64,
	)
}

// ChannelActivityPublisher is implemented by publishers that also tell every
// member who can read a channel that it received a new top-level message, so
// the channel list can show it as unread without subscribing to the channel.
type ChannelActivityPublisher interface {
	PublishChannelActivity(serverID int64, value Message)
}

func (s *Service) publishActivity(serverID int64, value Message) {
	if value.Replayed || value.ThreadRootID != nil {
		return
	}
	if publisher, ok := s.events.(ChannelActivityPublisher); ok {
		publisher.PublishChannelActivity(serverID, value)
	}
}

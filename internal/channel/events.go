package channel

type EventPublisher interface {
	PublishChannelCreated(channel Channel)

	PublishChannelUpdated(channel Channel)

	PublishChannelDeleted(channel Channel)
}

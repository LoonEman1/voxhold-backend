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

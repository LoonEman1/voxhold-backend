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

	PublishMessagePinned(value Pin)

	PublishMessageUnpinned(
		channelID int64,
		messageID int64,
	)
}

package message

type EventPublisher interface {
	PublishMessageCreated(
		value Message,
	)
}

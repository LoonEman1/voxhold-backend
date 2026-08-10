package readstate

type EventPublisher interface {
	PublishChannelRead(read ChannelRead)
}

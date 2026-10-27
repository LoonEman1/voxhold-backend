package realtime

import "voxhold-backend/internal/message"

const EventMessageReactionsChanged EventType = "message.reactions_changed"

// Personalization is done from one committed snapshot, with no per-recipient
// SQL. Multiple tabs of the same account reuse the same immutable event.
func (p *MessageEventPublisher) PublishReactionsChanged(state message.ReactionState) {
	room := p.hub.getRoom(state.ChannelID)
	if room == nil {
		return
	}
	events := map[int64]OutgoingEvent{}
	for _, client := range room.snapshot() {
		event, ok := events[client.UserID()]
		if !ok {
			event = OutgoingEvent{Type: EventMessageReactionsChanged, Data: state.ForUser(client.UserID())}
			events[client.UserID()] = event
		}
		if !client.enqueue(event) {
			p.hub.Unregister(client)
		}
	}
}

package realtime

import (
	"testing"
	"voxhold-backend/internal/message"
	"voxhold-backend/internal/notifications"
)

func TestMessageNotificationRunsAfterChatAndDoesNotRepeatForReplayedSend(t *testing.T) {
	hub := NewHub()
	defer hub.Close()
	client := NewClient(2, "session", []int64{1})
	hub.Register(client)
	hub.Subscribe(client, 1, 7)
	for {
		if _, ok := client.PopOutgoing(); !ok {
			break
		}
	}
	count := 0
	publisher := NewMessageEventPublisher(hub)
	publisher.SetCreatedListener(func(message.Message) {
		count++
		event, ok := client.PopOutgoing()
		if !ok || event.Type != EventMessageCreated {
			t.Fatal("notification work preceded chat delivery")
		}
	})
	value := message.Message{ID: 1, ChannelID: 7, Content: "<@2>"}
	publisher.PublishMessageCreated(value)
	value.Replayed = true
	publisher.PublishMessageCreated(value)
	publisher.PublishMessageUpdated(value)
	if count != 1 {
		t.Fatalf("alerts=%d", count)
	}
}

func TestNotificationQueueCoalescesWithoutDisconnectingOrBlockingCriticalEvents(t *testing.T) {
	client := NewClient(2, "session", []int64{1})
	for id := int64(1); id < 1000; id++ {
		if !client.enqueue(OutgoingEvent{Type: EventNotificationMessage, Data: notifications.Event{ChannelID: 7, Message: notifications.Preview{ID: id}}}) {
			t.Fatal("notification overflow")
		}
	}
	if client.QueueStats().NormalPending != 1 {
		t.Fatal("alerts were not coalesced")
	}
	event, _ := client.PopOutgoing()
	if event.Data.(notifications.Event).Message.ID != 999 {
		t.Fatal("latest alert lost")
	}
	for id := int64(1); id <= outgoingBufferSize; id++ {
		client.enqueue(OutgoingEvent{Type: EventNotificationMessage, Data: notifications.Event{ChannelID: id}})
	}
	if !client.enqueue(OutgoingEvent{Type: EventNotificationMessage, Data: notifications.Event{ChannelID: 1000}}) {
		t.Fatal("transient alert closed queue")
	}
	if !client.enqueue(OutgoingEvent{Type: EventChannelsChanged, Data: ServerDeletedData{ServerID: 1}}) {
		t.Fatal("alerts blocked a permission event")
	}
}

func TestQueuedNotificationsRespectRevocation(t *testing.T) {
	client := NewClient(2, "session", []int64{1})
	client.replaceChannelAccess(map[int64]struct{}{7: {}})
	client.enqueue(OutgoingEvent{Type: EventNotificationMessage, Data: notifications.Event{ChannelID: 7, Message: notifications.Preview{ID: 1, Content: "private"}}})
	client.replaceChannelAccess(map[int64]struct{}{})
	if _, ok := client.PopOutgoing(); ok {
		t.Fatal("private notification remained queued")
	}
	if !client.enqueue(OutgoingEvent{Type: EventNotificationMessage, Data: notifications.Event{ChannelID: 7}}) {
		t.Fatal("filtered event should not disconnect client")
	}
	if _, ok := client.PopOutgoing(); ok {
		t.Fatal("private notification delivered")
	}
}

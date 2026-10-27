package realtime

import "testing"

func TestQueueTriesOtherChannelsWhenFirstCompactionDoesNotFreeSpace(t *testing.T) {
	client := NewClient(1, "session", []int64{1})
	client.EnableMessageSync()
	client.addSubscription(1, 2)
	client.addSubscription(1, 3)
	client.Send(OutgoingEvent{Type: EventMessageCreated, Data: MessageCreatedData{ChannelID: 2}})
	for i := 1; i < outgoingBufferSize; i++ {
		client.Send(OutgoingEvent{Type: EventMessageCreated, Data: MessageCreatedData{ChannelID: 3}})
	}
	if !client.Send(OutgoingEvent{Type: EventServerMemberRemoved}) {
		t.Fatal("a compactable second channel must leave room for the permission change")
	}
	critical := 0
	for {
		event, ok := client.PopOutgoing()
		if !ok {
			break
		}
		if event.Type == EventServerMemberRemoved {
			critical++
		}
	}
	if critical != 1 {
		t.Fatal(critical)
	}
}

func TestChatBurstResyncPreservesVoiceAndCriticalSignaling(t *testing.T) {
	hub := NewHub()
	client := NewClient(1, "session", []int64{1})
	client.EnableMessageSync()
	hub.Register(client)
	hub.Subscribe(client, 1, 2)
	drainOutgoingEvents(client)
	for id := int64(1); id <= 2000; id++ {
		if hub.Publish(2, OutgoingEvent{Type: EventMessageCreated, Data: MessageCreatedData{ID: id, ChannelID: 2}}) != 1 {
			t.Fatal("chat burst disconnected client", id)
		}
	}
	if !client.Send(OutgoingEvent{Type: EventVoiceWebRTCOffer, Data: VoiceWebRTCOfferData{SDP: "offer"}}) {
		t.Fatal("offer rejected")
	}
	if !client.Send(OutgoingEvent{Type: EventVoiceICECandidate, Data: VoiceICECandidateData{Candidate: "ice"}}) {
		t.Fatal("ICE rejected")
	}
	if nextOutgoingEvent(t, client).Type != EventVoiceWebRTCOffer || nextOutgoingEvent(t, client).Type != EventVoiceICECandidate {
		t.Fatal("media was stuck behind chat")
	}
	marker := nextOutgoingEvent(t, client)
	if marker.Type != EventChannelSyncRequired || marker.Data.(ChannelSubscriptionData).ChannelID != 2 {
		t.Fatal(marker)
	}
	stats := client.QueueStats()
	if stats.Resyncs == 0 || stats.HighWatermark > outgoingBufferSize*2 {
		t.Fatal(stats)
	}
	select {
	case <-client.Done():
		t.Fatal("client disconnected")
	default:
	}
}

func TestQueueCoalescesOnlyReplaceableStateAndBoundsMedia(t *testing.T) {
	client := NewClient(1, "session", nil)
	for id := int64(1); id <= 500; id++ {
		if !client.Send(OutgoingEvent{Type: EventChannelRead, Data: ChannelReadData{ChannelID: 2, UserID: 3, LastReadMessageID: id}}) {
			t.Fatal(id)
		}
	}
	client.Send(OutgoingEvent{Type: EventChannelRead, Data: ChannelReadData{ChannelID: 2, UserID: 3, LastReadMessageID: 1}})
	if event := nextOutgoingEvent(t, client); event.Data.(ChannelReadData).LastReadMessageID != 500 {
		t.Fatal(event)
	}
	for i := 0; i < outgoingBufferSize; i++ {
		if !client.Send(OutgoingEvent{Type: EventStreamICECandidate}) {
			t.Fatal(i)
		}
	}
	if client.Send(OutgoingEvent{Type: EventStreamICECandidate}) {
		t.Fatal("unbounded media queue")
	}
}

func TestLegacyClientsNeverSilentlyLoseChatEvents(t *testing.T) {
	client := NewClient(1, "session", []int64{1})
	client.addSubscription(1, 2)
	for i := 0; i < outgoingBufferSize; i++ {
		if !client.Send(OutgoingEvent{Type: EventMessageCreated, Data: MessageCreatedData{ChannelID: 2}}) {
			t.Fatal(i)
		}
	}
	if client.Send(OutgoingEvent{Type: EventMessageCreated, Data: MessageCreatedData{ChannelID: 2}}) {
		t.Fatal("legacy client was sent an unsupported sync marker")
	}
}

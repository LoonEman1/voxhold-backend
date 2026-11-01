package realtime

import (
	"testing"

	"voxhold-backend/internal/message"
)

const (
	deviceA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	deviceB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func eventsOf(client *Client, eventType EventType) []OutgoingEvent {
	var events []OutgoingEvent
	for {
		event, ok := client.PopOutgoing()
		if !ok {
			return events
		}
		if event.Type == eventType {
			events = append(events, event)
		}
	}
}

func TestE2EEKeysAreRelayedOnlyInsideTheEncryptedRoom(t *testing.T) {
	hub := NewHub()
	alice := NewClient(1, "alice", []int64{10})
	bob := NewClient(2, "bob", []int64{10})
	carol := NewClient(3, "carol", []int64{10})
	for _, client := range []*Client{alice, bob, carol} {
		hub.Register(client)
	}
	hub.JoinVoiceWithDevice(alice, 10, 100, false, false, deviceA)
	hub.JoinVoiceWithDevice(bob, 10, 100, false, false, deviceB)
	hub.JoinVoiceWithDevice(carol, 10, 101, false, false, deviceB)
	for _, client := range []*Client{alice, bob, carol} {
		drainOutgoingEvents(client)
	}

	key := E2EERelayData{ChannelID: 100, RecipientUserID: 2, RecipientDeviceID: deviceB, Payload: "c2VjcmV0"}
	if !hub.RelayVoiceE2EE(alice, EventE2EEKey, key) {
		t.Fatal("key for a room member was not relayed")
	}
	relayed := eventsOf(bob, EventE2EEKey)
	if len(relayed) != 1 {
		t.Fatalf("bob received %d keys", len(relayed))
	}
	data := relayed[0].Data.(E2EERelayData)
	if data.SenderUserID != 1 || data.SenderDeviceID != deviceA || data.SenderConnectionID != alice.ConnectionID() || data.Payload != "c2VjcmV0" || data.RecipientDeviceID != "" {
		t.Fatalf("relayed data = %+v", data)
	}

	for name, attempt := range map[string]E2EERelayData{
		"wrong device":      {ChannelID: 100, RecipientUserID: 2, RecipientDeviceID: deviceA, Payload: "eA=="},
		"other channel":     {ChannelID: 100, RecipientUserID: 3, RecipientDeviceID: deviceB, Payload: "eA=="},
		"sender not inside": {ChannelID: 101, RecipientUserID: 3, RecipientDeviceID: deviceB, Payload: "eA=="},
		"to self":           {ChannelID: 100, RecipientUserID: 1, RecipientDeviceID: deviceA, Payload: "eA=="},
	} {
		if hub.RelayVoiceE2EE(alice, EventE2EEKey, attempt) {
			t.Errorf("%s: relayed", name)
		}
	}
	if len(eventsOf(carol, EventE2EEKey)) != 0 {
		t.Fatal("a client outside the room received a key")
	}

	hub.LeaveVoice(bob)
	if hub.RelayVoiceE2EE(alice, EventE2EEKey, key) {
		t.Fatal("key relayed to a participant who left")
	}
}

func TestPlainParticipantsCannotExchangeKeys(t *testing.T) {
	hub := NewHub()
	alice := NewClient(1, "alice", []int64{10})
	bob := NewClient(2, "bob", []int64{10})
	hub.Register(alice)
	hub.Register(bob)
	hub.JoinVoice(alice, 10, 100, false, false)
	hub.JoinVoiceWithDevice(bob, 10, 100, false, false, deviceB)
	if hub.RelayVoiceE2EE(alice, EventE2EEKey, E2EERelayData{ChannelID: 100, RecipientUserID: 2, RecipientDeviceID: deviceB, Payload: "eA=="}) {
		t.Fatal("a sender without a device relayed a key")
	}
}

func TestResetVoiceChannelDisconnectsEveryParticipant(t *testing.T) {
	hub := NewHub()
	alice := NewClient(1, "alice", []int64{10})
	bob := NewClient(2, "bob", []int64{10})
	hub.Register(alice)
	hub.Register(bob)
	hub.JoinVoice(alice, 10, 100, false, false)
	hub.JoinVoice(bob, 10, 101, false, false)
	drainOutgoingEvents(alice)
	drainOutgoingEvents(bob)

	hub.ResetVoiceChannel(100, VoiceWebRTCClosedReasonE2EEPolicy)

	closed := eventsOf(alice, EventVoiceWebRTCClosed)
	if len(closed) != 1 || closed[0].Data.(VoiceWebRTCClosedData).Reason != VoiceWebRTCClosedReasonE2EEPolicy {
		t.Fatalf("alice close events = %+v", closed)
	}
	if _, inside := hub.voice.current(alice); inside {
		t.Fatal("alice is still in the channel")
	}
	if _, inside := hub.voice.current(bob); !inside {
		t.Fatal("a participant of another channel was disconnected")
	}
}

func TestChannelActivityReachesMembersWithoutSubscription(t *testing.T) {
	hub := NewHub()
	reader := NewClient(1, "reader", []int64{10})
	restricted := NewClient(2, "restricted", []int64{10})
	hub.Register(reader)
	hub.Register(restricted)
	restricted.replaceChannelAccess(map[int64]struct{}{8: {}})
	drainOutgoingEvents(reader)
	drainOutgoingEvents(restricted)

	publisher := NewMessageEventPublisher(hub)
	publisher.PublishChannelActivity(10, message.Message{ID: 55, ChannelID: 7, Author: message.Author{UserID: 3}})

	got := eventsOf(reader, EventChannelActivity)
	if len(got) != 1 || got[0].Data.(ChannelActivityData) != (ChannelActivityData{ServerID: 10, ChannelID: 7, MessageID: 55, AuthorUserID: 3}) {
		t.Fatalf("reader activity = %+v", got)
	}
	if len(eventsOf(restricted, EventChannelActivity)) != 0 {
		t.Fatal("activity leaked to a member without channel access")
	}
}

func TestChannelActivityCoalescesPerChannel(t *testing.T) {
	client := NewClient(1, "reader", []int64{10})
	for id := int64(1); id <= 5; id++ {
		client.enqueue(OutgoingEvent{Type: EventChannelActivity, Data: ChannelActivityData{ServerID: 10, ChannelID: 7, MessageID: id}})
	}
	got := eventsOf(client, EventChannelActivity)
	if len(got) != 1 || got[0].Data.(ChannelActivityData).MessageID != 5 {
		t.Fatalf("activity = %+v", got)
	}
}

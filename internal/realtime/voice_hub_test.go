package realtime

import "testing"

func TestHubVoiceLifecycle(t *testing.T) {
	hub := NewHub()
	participant := NewClient(1, "participant", []int64{10})
	observer := NewClient(2, "observer", []int64{10})

	hub.Register(participant)
	hub.Register(observer)
	drainOutgoingEvents(participant)
	drainOutgoingEvents(observer)

	joined, ok := hub.JoinVoice(
		participant,
		10,
		100,
		false,
		false,
	)
	if !ok {
		t.Fatal("join voice room")
	}
	if len(joined.Participants) != 1 ||
		joined.Participant.ChannelID != 100 {

		t.Fatalf("unexpected join result: %+v", joined)
	}

	assertVoiceParticipantEvent(
		t,
		observer,
		EventVoiceParticipantJoined,
		1,
		10,
		100,
		false,
		false,
	)

	updated, ok := hub.UpdateVoiceState(
		participant,
		true,
		true,
	)
	if !ok || !updated.SelfMute || !updated.SelfDeaf {
		t.Fatalf("unexpected voice update: %+v", updated)
	}

	assertVoiceParticipantEvent(
		t,
		observer,
		EventVoiceStateUpdated,
		1,
		10,
		100,
		true,
		true,
	)

	left, ok := hub.LeaveVoice(participant)
	if !ok || left.ChannelID != 100 {
		t.Fatalf("unexpected voice leave: %+v", left)
	}

	assertVoiceLeftEvent(t, observer, 1, 10, 100)

	if _, ok := hub.LeaveVoice(participant); ok {
		t.Fatal("second voice leave unexpectedly succeeded")
	}
}

func TestHubVoiceMoveAndDisconnect(t *testing.T) {
	hub := NewHub()
	participant := NewClient(1, "participant", []int64{10})
	observer := NewClient(2, "observer", []int64{10})

	hub.Register(participant)
	hub.Register(observer)
	drainOutgoingEvents(participant)
	drainOutgoingEvents(observer)

	hub.JoinVoice(participant, 10, 100, false, false)
	drainOutgoingEvents(observer)

	joined, ok := hub.JoinVoice(
		participant,
		10,
		101,
		false,
		false,
	)
	if !ok || joined.Participant.ChannelID != 101 {
		t.Fatalf("unexpected moved participant: %+v", joined)
	}

	assertVoiceLeftEvent(t, observer, 1, 10, 100)
	assertVoiceParticipantEvent(
		t,
		observer,
		EventVoiceParticipantJoined,
		1,
		10,
		101,
		false,
		false,
	)

	hub.Unregister(participant)

	assertVoiceLeftEvent(t, observer, 1, 10, 101)
	assertPresenceUpdatedEvent(
		t,
		observer,
		10,
		1,
		PresenceOffline,
	)
}

func TestHubVoiceConnectionTakeover(t *testing.T) {
	hub := NewHub()
	closer := &voiceSessionCloserStub{}
	hub.SetVoiceSessionCloser(closer)

	oldClient := NewClient(1, "old-session", []int64{10})
	replacement := NewClient(1, "new-session", []int64{10})
	observer := NewClient(2, "observer", []int64{10})

	hub.Register(oldClient)
	hub.Register(replacement)
	hub.Register(observer)
	drainOutgoingEvents(oldClient)
	drainOutgoingEvents(replacement)
	drainOutgoingEvents(observer)

	hub.JoinVoice(oldClient, 10, 100, false, false)
	drainOutgoingEvents(replacement)
	drainOutgoingEvents(observer)

	joined, ok := hub.JoinVoice(
		replacement,
		10,
		100,
		true,
		false,
	)
	if !ok || len(joined.Participants) != 1 {
		t.Fatalf("unexpected takeover result: %+v", joined)
	}
	if joined.Participant.ConnectionID != replacement.ConnectionID() {
		t.Fatalf("unexpected active connection: %+v", joined.Participant)
	}

	assertVoiceLeftEvent(t, oldClient, 1, 10, 100)
	assertVoiceClosedEvent(
		t,
		oldClient,
		VoiceWebRTCClosedReasonReplaced,
	)
	assertVoiceParticipantEvent(
		t,
		oldClient,
		EventVoiceParticipantJoined,
		1,
		10,
		100,
		true,
		false,
	)

	assertVoiceLeftEvent(t, observer, 1, 10, 100)
	assertVoiceParticipantEvent(
		t,
		observer,
		EventVoiceParticipantJoined,
		1,
		10,
		100,
		true,
		false,
	)

	if len(closer.connectionIDs) != 1 ||
		closer.connectionIDs[0] != oldClient.ConnectionID() {

		t.Fatalf(
			"replaced media session was not closed: %v",
			closer.connectionIDs,
		)
	}

	if _, ok := hub.LeaveVoice(oldClient); ok {
		t.Fatal("replaced connection retained voice state")
	}

	hub.Unregister(oldClient)
	updated, ok := hub.UpdateVoiceState(replacement, false, true)
	if !ok || updated.ConnectionID != replacement.ConnectionID() {
		t.Fatalf("replacement was removed by stale disconnect: %+v", updated)
	}
}

func TestHubVoiceSnapshotAndChannelRemoval(t *testing.T) {
	hub := NewHub()
	closer := &voiceSessionCloserStub{}
	hub.SetVoiceSessionCloser(closer)
	participant := NewClient(1, "participant", []int64{10})

	hub.Register(participant)
	drainOutgoingEvents(participant)
	hub.JoinVoice(participant, 10, 100, false, false)

	newcomer := NewClient(2, "newcomer", []int64{10})
	hub.Register(newcomer)

	var snapshot VoiceSnapshotData
	for {
		event := nextOutgoingEvent(t, newcomer)
		if event.Type != EventVoiceSnapshot {
			continue
		}

		var ok bool
		snapshot, ok = event.Data.(VoiceSnapshotData)
		if !ok {
			t.Fatalf("unexpected snapshot data: %T", event.Data)
		}
		break
	}

	if len(snapshot.Participants) != 1 ||
		snapshot.Participants[0].UserID != 1 ||
		snapshot.Participants[0].ChannelID != 100 {

		t.Fatalf("unexpected voice snapshot: %+v", snapshot)
	}

	hub.RemoveChannel(100)

	if _, ok := hub.LeaveVoice(participant); ok {
		t.Fatal("removed channel retained voice participant")
	}

	if len(closer.connectionIDs) != 1 ||
		closer.connectionIDs[0] != participant.ConnectionID() {

		t.Fatalf(
			"voice media session was not closed: %v",
			closer.connectionIDs,
		)
	}
}

type voiceSessionCloserStub struct {
	connectionIDs []string
}

func (s *voiceSessionCloserStub) Leave(connectionID string) {
	s.connectionIDs = append(s.connectionIDs, connectionID)
}

func assertVoiceParticipantEvent(
	t *testing.T,
	client *Client,
	eventType EventType,
	userID int64,
	serverID int64,
	channelID int64,
	selfMute bool,
	selfDeaf bool,
) {
	t.Helper()

	event := nextOutgoingEvent(t, client)
	if event.Type != eventType {
		t.Fatalf("unexpected event type: %s", event.Type)
	}

	data, ok := event.Data.(VoiceParticipantData)
	if !ok {
		t.Fatalf("unexpected event data: %T", event.Data)
	}

	if data.UserID != userID ||
		data.ServerID != serverID ||
		data.ChannelID != channelID ||
		data.SelfMute != selfMute ||
		data.SelfDeaf != selfDeaf {

		t.Fatalf("unexpected participant data: %+v", data)
	}
}

func assertVoiceLeftEvent(
	t *testing.T,
	client *Client,
	userID int64,
	serverID int64,
	channelID int64,
) {
	t.Helper()

	event := nextOutgoingEvent(t, client)
	if event.Type != EventVoiceParticipantLeft {
		t.Fatalf("unexpected event type: %s", event.Type)
	}

	data, ok := event.Data.(VoiceLeftData)
	if !ok {
		t.Fatalf("unexpected event data: %T", event.Data)
	}

	if data.UserID != userID ||
		data.ServerID != serverID ||
		data.ChannelID != channelID {

		t.Fatalf("unexpected voice left data: %+v", data)
	}
}

func assertVoiceClosedEvent(
	t *testing.T,
	client *Client,
	reason string,
) {
	t.Helper()

	event := nextOutgoingEvent(t, client)
	if event.Type != EventVoiceWebRTCClosed {
		t.Fatalf("unexpected event type: %s", event.Type)
	}

	data, ok := event.Data.(VoiceWebRTCClosedData)
	if !ok {
		t.Fatalf("unexpected event data: %T", event.Data)
	}
	if data.Reason != reason {
		t.Fatalf("unexpected close reason: %q", data.Reason)
	}
}

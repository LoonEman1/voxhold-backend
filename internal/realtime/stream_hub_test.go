package realtime

import (
	"testing"
	"time"
)

func TestStreamRequiresSameVoiceChannel(t *testing.T) {
	hub := NewHub()
	publisher := NewClient(1, "publisher", []int64{10})
	viewer := NewClient(2, "viewer", []int64{10})
	hub.Register(publisher)
	hub.Register(viewer)

	if _, err := hub.StartStream(
		publisher, 10, 100, StreamModeServer, StreamCodecVP9, true,
	); err != ErrStreamVoiceRequired {

		t.Fatalf("stream without voice unexpectedly accepted: %v", err)
	}
	hub.JoinVoice(publisher, 10, 100, false, false)
	streamData, err := hub.StartStream(
		publisher, 10, 100, StreamModeServer, StreamCodecVP9, true,
	)
	if err != nil || streamData.ChannelID != 100 {
		t.Fatalf("start stream in voice room: %+v, %v", streamData, err)
	}

	hub.JoinVoice(viewer, 10, 101, false, false)
	if _, err := hub.WatchStream(viewer, 10, 100); err != ErrStreamVoiceRequired {
		t.Fatalf("cross-channel viewer unexpectedly accepted: %v", err)
	}
	hub.JoinVoice(viewer, 10, 100, false, false)
	watching, err := hub.WatchStream(viewer, 10, 100)
	if err != nil || watching.Stream.ViewerCount != 1 {
		t.Fatalf("watch same voice room: %+v, %v", watching, err)
	}

	hub.LeaveVoice(publisher)
	if len(hub.streams.snapshotForServers([]int64{10})) != 0 {
		t.Fatal("publisher voice leave retained active stream")
	}
}

func TestP2PStreamSignalingOnlyAllowsPublisherViewerPair(t *testing.T) {
	hub := NewHub()
	publisher := NewClient(1, "publisher", []int64{10})
	viewer := NewClient(2, "viewer", []int64{10})
	outsider := NewClient(3, "outsider", []int64{10})
	for _, client := range []*Client{publisher, viewer, outsider} {
		hub.Register(client)
		hub.JoinVoice(client, 10, 100, false, false)
	}
	if _, err := hub.StartStream(publisher, 10, 100, StreamModeP2P, StreamCodecVP8, false); err != nil {
		t.Fatal(err)
	}
	if _, err := hub.WatchStream(viewer, 10, 100); err != nil {
		t.Fatal(err)
	}
	if err := hub.RelayStreamP2PSession(
		publisher,
		viewer.ConnectionID(),
		"offer",
		EventStreamP2POffer,
	); err != nil {
		t.Fatalf("relay valid offer: %v", err)
	}
	if err := hub.RelayStreamP2PSession(
		outsider,
		viewer.ConnectionID(),
		"offer",
		EventStreamP2POffer,
	); err != ErrStreamP2PRelation {
		t.Fatalf("outsider signaling unexpectedly accepted: %v", err)
	}
	drainTestEvents(publisher.Outgoing())
	if err := hub.RequestStreamP2PRestart(
		viewer,
		publisher.ConnectionID(),
	); err != nil {
		t.Fatalf("viewer restart request rejected: %v", err)
	}
	select {
	case event := <-publisher.Outgoing():
		viewerData, ok := event.Data.(StreamViewerData)
		if event.Type != EventStreamP2PRestart || !ok ||
			viewerData.ConnectionID != viewer.ConnectionID() ||
			viewerData.UserID != viewer.UserID() {

			t.Fatalf("unexpected P2P restart event: %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("publisher did not receive P2P restart request")
	}
	if err := hub.RequestStreamP2PRestart(
		publisher,
		viewer.ConnectionID(),
	); err != ErrStreamP2PRelation {
		t.Fatalf("publisher restart request unexpectedly accepted: %v", err)
	}
	if err := hub.RequestStreamP2PRestart(
		outsider,
		publisher.ConnectionID(),
	); err != ErrStreamP2PRelation {
		t.Fatalf("outsider restart request unexpectedly accepted: %v", err)
	}
}

func drainTestEvents(events <-chan OutgoingEvent) {
	for {
		select {
		case <-events:
		default:
			return
		}
	}
}

package realtime

import "testing"

func TestLegacyStreamCatalogKeepsItsPrimaryAndIgnoresForeignStops(t *testing.T) {
	client := NewClient(1, "legacy", []int64{10})
	first := StreamData{ServerID: 10, ChannelID: 100, PublisherConnectionID: "first"}
	second := StreamData{ServerID: 10, ChannelID: 100, PublisherConnectionID: "second"}
	client.enqueue(OutgoingEvent{Type: EventStreamSnapshot, Data: StreamSnapshotData{Streams: []StreamData{first, second}}})
	event, ok := client.PopOutgoing()
	if !ok || len(event.Data.(StreamSnapshotData).Streams) != 1 || event.Data.(StreamSnapshotData).Streams[0].PublisherConnectionID != "first" {
		t.Fatal("legacy snapshot contains multiple publishers")
	}
	client.enqueue(OutgoingEvent{Type: EventStreamStarted, Data: second})
	client.enqueue(OutgoingEvent{Type: EventStreamUpdated, Data: second})
	client.enqueue(OutgoingEvent{Type: EventStreamStopped, Data: StreamStoppedData{ServerID: 10, ChannelID: 100, PublisherConnectionID: "second"}})
	if event, exists := client.PopOutgoing(); exists {
		t.Fatalf("foreign stream changed legacy playback: %+v", event)
	}
	client.enqueue(OutgoingEvent{Type: EventStreamLeft, Data: StreamStoppedData{ServerID: 10, ChannelID: 100, PublisherConnectionID: "first"}})
	client.PopOutgoing()
	if client.legacyStreams[100] != "first" {
		t.Fatal("viewer leave erased an active channel catalog entry")
	}
	client.enqueue(OutgoingEvent{Type: EventStreamStopped, Data: StreamStoppedData{ServerID: 10, ChannelID: 100, PublisherConnectionID: "first"}})
	client.PopOutgoing()
	client.enqueue(OutgoingEvent{Type: EventStreamStarted, Data: second})
	event, ok = client.PopOutgoing()
	if !ok || event.Data.(StreamData).PublisherConnectionID != "second" {
		t.Fatal("legacy primary handover failed")
	}
}

func TestMultipleStreamCapabilityReceivesFullSnapshotAndScopedStops(t *testing.T) {
	client := NewClient(1, "current", []int64{10})
	client.EnableMultipleStreams()
	client.enqueue(OutgoingEvent{Type: EventStreamSnapshot, Data: StreamSnapshotData{Streams: []StreamData{{ChannelID: 100, PublisherConnectionID: "first"}, {ChannelID: 100, PublisherConnectionID: "second"}}}})
	event, exists := client.PopOutgoing()
	if !exists || len(event.Data.(StreamSnapshotData).Streams) != 2 {
		t.Fatal("new client lost a publisher")
	}
	client.enqueue(OutgoingEvent{Type: EventStreamStopped, Data: StreamStoppedData{ChannelID: 100, PublisherConnectionID: "second"}})
	if _, exists := client.PopOutgoing(); !exists {
		t.Fatal("new client lost a scoped stop")
	}
}

func TestOldStreamFailureDoesNotCloseNewPublisherSelection(t *testing.T) {
	hub := NewHub()
	defer hub.Close()
	clients := []*Client{NewClient(1, "a", []int64{10}), NewClient(2, "b", []int64{10}), NewClient(3, "viewer", []int64{10})}
	for _, client := range clients {
		client.EnableMultipleStreams()
		hub.Register(client)
		hub.JoinVoice(client, 10, 100, false, false)
	}
	for _, client := range clients[:2] {
		if _, err := hub.StartStream(client, 10, 100, StreamModeServer, StreamCodecVP8, false); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := hub.WatchStreamWithCapabilities(clients[2], 10, 100, nil, nil, clients[0].ConnectionID()); err != nil {
		t.Fatal(err)
	}
	stale := NewStreamSignalSink(hub).SessionFailureHandler(clients[2].ConnectionID())
	hub.LeaveStream(clients[2], "switch")
	if _, err := hub.WatchStreamWithCapabilities(clients[2], 10, 100, nil, nil, clients[1].ConnectionID()); err != nil {
		t.Fatal(err)
	}
	stale("old peer failed")
	value, ok := hub.streams.recoveryViewer(clients[2], 10, 100)
	if !ok || value.publisher != clients[1] {
		t.Fatal("stale failure closed a different publisher selection")
	}
}

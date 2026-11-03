package realtime

import (
	"errors"
	"fmt"
	"testing"
	"voxhold-backend/internal/streamcontract"
)

func TestMultipleChannelStreamsKeepViewersAndSignalingIsolated(t *testing.T) {
	for _, mode := range []StreamMode{StreamModeServer, StreamModeP2P} {
		t.Run(string(mode), func(t *testing.T) {
			hub := NewHub()
			defer hub.Close()
			clients := make([]*Client, 4)
			for i := range clients {
				clients[i] = NewClient(int64(i+1), fmt.Sprintf("user-%d", i), []int64{10})
				clients[i].EnableMultipleStreams()
				hub.Register(clients[i])
				hub.JoinVoice(clients[i], 10, 100, false, false)
			}
			first, err := hub.StartStream(clients[0], 10, 100, mode, StreamCodecVP8, true)
			if err != nil {
				t.Fatal(err)
			}
			second, err := hub.StartStream(clients[1], 10, 100, mode, StreamCodecVP9, false)
			if err != nil {
				t.Fatal(err)
			}
			for i, publisher := range []string{first.PublisherConnectionID, second.PublisherConnectionID} {
				watching, err := hub.WatchStreamWithCapabilities(clients[i+2], 10, 100, nil, nil, publisher)
				if err != nil || watching.Stream.PublisherConnectionID != publisher || watching.Stream.ViewerCount != 1 {
					t.Fatalf("wrong stream selected: %+v %v", watching, err)
				}
			}
			if mode == StreamModeP2P {
				if err := hub.RelayStreamP2PSession(clients[0], clients[3].ConnectionID(), "foreign", EventStreamP2POffer); !errors.Is(err, ErrStreamP2PRelation) {
					t.Fatalf("cross-stream P2P offer accepted: %v", err)
				}
			}
			stopped, publisher := hub.LeaveStream(clients[0], "first screen stopped")
			if !publisher || stopped.PublisherConnectionID != first.PublisherConnectionID {
				t.Fatalf("stop lost identity: %+v", stopped)
			}
			snapshot := hub.streams.snapshotForServers([]int64{10})
			if len(snapshot) != 1 || snapshot[0].PublisherConnectionID != second.PublisherConnectionID || snapshot[0].ViewerCount != 1 {
				t.Fatalf("unrelated stream changed: %+v", snapshot)
			}
			hub.streams.mu.RLock()
			_, firstViewerActive := hub.streams.byClient[clients[2]]
			secondViewer := hub.streams.byClient[clients[3]]
			hub.streams.mu.RUnlock()
			if firstViewerActive || secondViewer.stream == nil || secondViewer.stream.publisher != clients[1] {
				t.Fatal("stopping one publisher affected the wrong viewers")
			}
			hub.stopStreamForChannel(100, "channel removed")
			if len(hub.streams.snapshotForServers([]int64{10})) != 0 {
				t.Fatal("channel teardown retained a publisher")
			}
		})
	}
}

func TestChannelPublisherCapacityAndLegacyWatch(t *testing.T) {
	hub := NewHub()
	defer hub.Close()
	clients := make([]*Client, streamcontract.MaxChannelPublishers+1)
	for i := range clients {
		clients[i] = NewClient(int64(i+1), "publisher", []int64{10})
		clients[i].EnableMultipleStreams()
		hub.Register(clients[i])
		hub.JoinVoice(clients[i], 10, 100, false, false)
		_, err := hub.StartStream(clients[i], 10, 100, StreamModeServer, StreamCodecVP8, false)
		if i < streamcontract.MaxChannelPublishers && err != nil {
			t.Fatal(err)
		}
		if i == streamcontract.MaxChannelPublishers && !errors.Is(err, ErrStreamPublisherLimit) {
			t.Fatalf("missing publisher bound: %v", err)
		}
	}
	viewer := NewClient(100, "viewer", []int64{10})
	hub.Register(viewer)
	hub.JoinVoice(viewer, 10, 100, false, false)
	if _, err := hub.WatchStreamWithCapabilities(viewer, 10, 100, nil, nil, "missing-publisher"); !errors.Is(err, ErrStreamUnavailable) {
		t.Fatalf("unknown identity fell back: %v", err)
	}
	watching, err := hub.WatchStream(viewer, 10, 100)
	if err != nil || watching.Stream.PublisherConnectionID != clients[0].ConnectionID() {
		t.Fatalf("legacy watch must choose the first active publisher: %+v %v", watching, err)
	}
	hub.LeaveStream(clients[0], "stopped")
	if _, err := hub.StartStream(clients[len(clients)-1], 10, 100, StreamModeServer, StreamCodecVP8, false); err != nil {
		t.Fatalf("released publisher slot not reusable: %v", err)
	}
}

func TestStreamPermissionRevocationKeepsOtherPublishersAlive(t *testing.T) {
	hub := NewHub()
	defer hub.Close()
	for _, id := range []int64{1, 2} {
		client := NewClient(id, "publisher", []int64{10})
		client.EnableMultipleStreams()
		hub.Register(client)
		hub.JoinVoice(client, 10, 100, false, false)
		if _, err := hub.StartStream(client, 10, 100, StreamModeServer, StreamCodecVP8, false); err != nil {
			t.Fatal(err)
		}
	}
	hub.SetRolePermissionLoader(func(_ int64, userID int64) (int, error) {
		if userID == 1 {
			return 7, nil
		}
		return 15, nil
	})
	hub.RefreshRolePermissions(10)
	remaining := hub.streams.snapshotForServers([]int64{10})
	if len(remaining) != 1 || remaining[0].PublisherUserID != 2 {
		t.Fatalf("wrong stream revoked: %+v", remaining)
	}
}

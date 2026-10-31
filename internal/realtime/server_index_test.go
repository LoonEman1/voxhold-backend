package realtime

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestServerIndexTracksMembershipRemovalAndUnregister(t *testing.T) {
	hub := NewHub()
	client := NewClient(1, "test", []int64{10, 20})
	hub.Register(client)
	drainOutgoingEvents(client)
	hub.RevokeUserFromServer(1, 10)
	drainOutgoingEvents(client)
	if got := hub.PublishToServer(10, OutgoingEvent{Type: "test"}); got != 0 {
		t.Fatal("removed server received", got)
	}
	if got := hub.PublishToServer(20, OutgoingEvent{Type: "test"}); got != 1 {
		t.Fatal("unrelated server lost", got)
	}
	hub.Unregister(client)
	if len(hub.clientsByServer) != 0 {
		t.Fatal("unregistered client retained in index")
	}
}

func TestPresencePersistsOncePerUserTransitionAcrossServers(t *testing.T) {
	hub := NewHub()
	var writes atomic.Int32
	hub.SetLastSeenSaver(func(int64, int64) { writes.Add(1) })
	client := NewClient(1, "test", []int64{1, 2, 3, 4, 5})
	hub.Register(client)
	if writes.Load() != 1 {
		t.Fatal("repeated online persistence", writes.Load())
	}
	hub.Unregister(client)
	if writes.Load() != 2 {
		t.Fatal("repeated offline persistence", writes.Load())
	}
	if len(hub.lastSeen) != 0 {
		t.Fatal("offline user stamp retained")
	}
}

func TestAddServerCannotReindexDisconnectedClient(t *testing.T) {
	hub := NewHub()
	client := NewClient(1, "test", []int64{10})
	hub.Register(client)
	loaded, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	hub.SetChannelAccessLoader(func(int64) (map[int64]struct{}, error) { close(loaded); <-release; return nil, nil })
	go func() { hub.AddUserToServer(1, 20); close(done) }()
	<-loaded
	hub.Unregister(client)
	close(release)
	<-done
	if len(hub.clientsByServer) != 0 || len(hub.presence) != 0 {
		t.Fatalf("disconnected client retained: servers=%d presence=%d", len(hub.clientsByServer), len(hub.presence))
	}
}

func TestRegisterDisconnectRaceLeavesNoPresenceOrIndex(t *testing.T) {
	for range 100 {
		hub := NewHub()
		client := NewClient(1, "test", []int64{10})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); hub.Register(client) }()
		go func() { defer wg.Done(); hub.Unregister(client) }()
		wg.Wait()
		if len(hub.clientsByServer) != 0 || len(hub.presence) != 0 {
			t.Fatal("registration raced back into a closed client")
		}
	}
}

func BenchmarkPublishToServer(b *testing.B) {
	hub := NewHub()
	for i := 0; i < 5000; i++ {
		serverID := int64(i%50 + 1)
		client := NewClient(int64(i+1), "test", []int64{serverID})
		hub.clients[client] = struct{}{}
		hub.indexServerLocked(serverID, client)
	}
	targets := hub.clientsForServer(1)
	event := OutgoingEvent{Type: "test"}
	for _, indexed := range []bool{false, true} {
		name := "InstanceScan"
		if indexed {
			name = "ServerIndex"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if indexed {
					hub.PublishToServer(1, event)
				} else {
					hub.clientsMu.RLock()
					clients := clientSetSnapshot(hub.clients)
					hub.clientsMu.RUnlock()
					selected := make([]*Client, 0, len(clients))
					for _, client := range clients {
						if client.hasServer(1) {
							selected = append(selected, client)
						}
					}
					hub.broadcastToClients(selected, event)
				}
				for _, client := range targets {
					client.PopOutgoing()
				}
			}
		})
	}
}

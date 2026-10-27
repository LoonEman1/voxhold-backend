package realtime

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDelayedLastSeenWriteCannotPublishOfflineAfterReconnect(t *testing.T) {
	hub := NewHub()
	observer := NewClient(2, "observer", []int64{10})
	first := NewClient(1, "first", []int64{10})
	second := NewClient(1, "second", []int64{10})
	blocked, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	hub.SetLastSeenSaver(func(id, at int64) {
		if id == 1 && calls.Add(1) == 2 {
			close(blocked)
			<-release
		}
	})
	hub.Register(observer)
	hub.Register(first)
	drainOutgoingEvents(observer)
	left, joined := make(chan struct{}), make(chan struct{})
	go func() { hub.Unregister(first); close(left) }()
	<-blocked
	go func() { hub.Register(second); close(joined) }()
	deadline := time.Now().Add(time.Second)
	for len(hub.onlineUserIDs(10)) < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	close(release)
	<-left
	<-joined
	assertPresenceUpdatedEvent(t, observer, 10, 1, PresenceOnline)
	assertNoOutgoingEvent(t, observer)
	hub.Close()
}

func TestLastSeenUsesPresenceTransitionsAcrossTabs(t *testing.T) {
	hub := NewHub()
	var mu sync.Mutex
	var times []int64
	hub.SetLastSeenSaver(func(id, at int64) {
		if id == 1 {
			mu.Lock()
			times = append(times, at)
			mu.Unlock()
		}
	})
	observer := NewClient(2, "observer", []int64{10})
	first := NewClient(1, "tab1", []int64{10})
	second := NewClient(1, "tab2", []int64{10})
	hub.Register(observer)
	drainOutgoingEvents(observer)
	hub.Register(first)
	drainOutgoingEvents(observer)
	hub.Register(second)
	hub.Unregister(first)
	if len(times) != 1 {
		t.Fatal("closing one tab changed last seen", times)
	}
	hub.Unregister(second)
	hub.Unregister(second)
	if len(times) != 2 || times[1] < times[0] {
		t.Fatal(times)
	}
	event := nextOutgoingEvent(t, observer)
	data := event.Data.(PresenceUpdatedData)
	if data.Status != PresenceOffline || data.LastSeenAt == nil || *data.LastSeenAt < time.Now().Add(-time.Minute).Unix() {
		t.Fatal(data)
	}
	hub.Close()
}

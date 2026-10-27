package realtime

import (
	"context"
	"errors"
	"sync"
	"testing"

	"voxhold-backend/internal/channel"
	channelsqlite "voxhold-backend/internal/channel/sqlite"
	"voxhold-backend/internal/recordings"
	"voxhold-backend/internal/testdb"
)

type accessCloser struct {
	mu  sync.Mutex
	ids map[string]bool
}

func TestAccessLoaderFailureClosesConnectionUntilPermissionsCanBeLoaded(t *testing.T) {
	hub := NewHub()
	defer hub.Close()
	failed := true
	hub.SetChannelAccessLoader(func(int64) (map[int64]struct{}, error) {
		if failed {
			return nil, errors.New("temporary failure")
		}
		return map[int64]struct{}{1: {}}, nil
	})
	first := NewClient(2, "first", []int64{1})
	if hub.Register(first) {
		t.Fatal("registered without a policy")
	}
	failed = false
	second := NewClient(2, "second", []int64{1})
	if !hub.Register(second) || !hub.Subscribe(second, 1, 1) {
		t.Fatal("reconnect failed")
	}
	failed = true
	hub.RefreshChannelAccess(1)
	select {
	case <-second.Done():
	default:
		t.Fatal("stale permissions survived a failed refresh")
	}
}

func (c *accessCloser) Leave(id string)       { c.mu.Lock(); defer c.mu.Unlock(); c.ids[id] = true }
func (c *accessCloser) closed(id string) bool { c.mu.Lock(); defer c.mu.Unlock(); return c.ids[id] }

func TestAccessRevocationClosesMediaPurgesQueuesAndFiltersSnapshots(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	repo := channelsqlite.NewRepository(db)
	hub := NewHub()
	hub.SetChannelAccessLoader(repo.AccessibleChannelIDs)
	defer hub.Close()
	voice := &accessCloser{ids: map[string]bool{}}
	streams := &accessCloser{ids: map[string]bool{}}
	hub.SetVoiceSessionCloser(voice)
	hub.SetStreamSessionCloser(streams, 32, 8)
	service := channel.NewService(repo, NewChannelEventPublisher(hub))
	owner := NewClient(1, "owner", []int64{1})
	member := NewClient(2, "member", []int64{1})
	if !hub.Register(owner) || !hub.Register(member) {
		t.Fatal("register")
	}
	hub.Subscribe(member, 1, 1)
	if _, ok := hub.JoinVoice(owner, 1, 1, false, false); !ok {
		t.Fatal("owner join")
	}
	if _, ok := hub.JoinVoice(member, 1, 1, false, false); !ok {
		t.Fatal("member join")
	}
	if _, err := hub.StartStream(owner, 1, 1, StreamModeServer, StreamCodecVP8, false); err != nil {
		t.Fatal(err)
	}
	if _, err := hub.WatchStream(member, 1, 1); err != nil {
		t.Fatal(err)
	}
	member.enqueue(OutgoingEvent{Type: EventMessageCreated, Data: MessageCreatedData{ChannelID: 1, Content: "private queued"}})
	if _, err := service.SetAccess(ctx, 1, 1, 1, channel.Access{Restricted: true}); err != nil {
		t.Fatal(err)
	}
	if _, ok := hub.voice.current(member); ok {
		t.Fatal("voice remained")
	}
	if !voice.closed(member.ConnectionID()) || !streams.closed(member.ConnectionID()) {
		t.Fatal("media session was not closed")
	}
	if hub.Subscribe(member, 1, 1) {
		t.Fatal("subscription accepted")
	}
	if _, ok := hub.JoinVoice(member, 1, 1, false, false); ok {
		t.Fatal("rejoin accepted")
	}
	if _, err := hub.WatchStream(member, 1, 1); err == nil {
		t.Fatal("rewatch accepted")
	}
	hub.PublishToServer(1, OutgoingEvent{Type: EventType("recording.updated"), Data: recordings.Record{ServerID: 1, ChannelID: 1}})
	for {
		event, ok := member.PopOutgoing()
		if !ok {
			break
		}
		if _, ok := member.filterChannelEvent(event); !ok {
			t.Fatalf("private event survived queue: %v", event.Type)
		}
		if event.Type == EventMessageCreated || event.Type == EventType("recording.updated") {
			t.Fatal("content leaked")
		}
		if data, ok := event.Data.(VoiceSnapshotData); ok && len(data.Participants) > 0 {
			t.Fatal("voice snapshot leaked")
		}
		if data, ok := event.Data.(StreamSnapshotData); ok && len(data.Streams) > 0 {
			t.Fatal("stream snapshot leaked")
		}
	}
	secondTab := NewClient(2, "second", []int64{1})
	hub.Register(secondTab)
	if secondTab.canAccessChannel(1) {
		t.Fatal("reconnect restored private access")
	}
	if _, err := service.SetAccess(ctx, 1, 1, 1, channel.Access{Restricted: true, UserIDs: []int64{2}}); err != nil {
		t.Fatal(err)
	}
	if !hub.Subscribe(member, 1, 1) || !secondTab.canAccessChannel(1) {
		t.Fatal("grant did not update both tabs")
	}
}

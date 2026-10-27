package realtime

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"voxhold-backend/internal/channel"
	channelsqlite "voxhold-backend/internal/channel/sqlite"
	"voxhold-backend/internal/recordings"
	"voxhold-backend/internal/testdb"
)

type accessCloser struct {
	mu  sync.Mutex
	ids map[string]bool
}

func TestBatchAccessDeduplicatesTabsAndFailsClosed(t *testing.T) {
	h := NewHub()
	defer h.Close()
	first, second := NewClient(2, "one", []int64{1}), NewClient(2, "two", []int64{1})
	h.Register(first)
	h.Register(second)
	first.enqueue(OutgoingEvent{Type: EventMessageCreated, Data: MessageCreatedData{ChannelID: 2, Content: "private"}})
	calls := 0
	h.SetChannelAccessBatchLoader(func(ids []int64) (map[int64]map[int64]struct{}, error) {
		calls++
		if len(ids) != 1 || ids[0] != 2 {
			t.Errorf("users were not deduplicated: %v", ids)
		}
		return nil, errors.New("unavailable")
	})
	h.RefreshChannelAccess(1)
	if calls != 1 {
		t.Fatalf("batch calls: %d", calls)
	}
	for _, client := range []*Client{first, second} {
		if client.canAccessChannel(2) {
			t.Fatal("loader error failed open")
		}
		select {
		case <-client.Done():
		default:
			t.Fatal("failed client stayed connected")
		}
		for {
			event, ok := client.PopOutgoing()
			if !ok {
				break
			}
			if event.Type == EventMessageCreated {
				t.Fatal("queued private content retained")
			}
		}
	}
}

func TestAccessRemovalWaitsForMediaSetup(t *testing.T) {
	for name, revoke := range map[string]func(*Hub){
		"membership": func(h *Hub) { h.RevokeUserFromServer(2, 1) },
		"server":     func(h *Hub) { h.RevokeServer(1) },
		"channel":    func(h *Hub) { h.RemoveChannel(1) },
	} {
		t.Run(name, func(t *testing.T) {
			hub := NewHub()
			defer hub.Close()
			release := sync.OnceFunc(hub.LockChannelOperation())
			defer release()
			done := make(chan struct{})
			go func() {
				revoke(hub)
				close(done)
			}()
			select {
			case <-done:
				t.Fatal("revocation completed before pending media setup")
			case <-time.After(30 * time.Millisecond):
			}
			release()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("revocation did not finish")
			}
		})
	}
}

func TestServerRevocationPurgesCachedAccessAndQueuedContent(t *testing.T) {
	for _, wholeServer := range []bool{false, true} {
		t.Run(map[bool]string{false: "membership", true: "server"}[wholeServer], func(t *testing.T) {
			db := testdb.Open(t)
			hub := NewHub()
			defer hub.Close()
			hub.SetChannelAccessLoader(channelsqlite.NewRepository(db).AccessibleChannelIDs)
			member := NewClient(2, "member", []int64{1})
			if !hub.Register(member) {
				t.Fatal("register")
			}
			member.enqueue(OutgoingEvent{Type: EventMessageCreated, Data: MessageCreatedData{ChannelID: 2, Content: "private queued"}})
			query := "DELETE FROM server_members WHERE server_id=1 AND user_id=2"
			if wholeServer {
				query = "DELETE FROM servers WHERE id=1"
			}
			if _, err := db.Exec(query); err != nil {
				t.Fatal(err)
			}
			if wholeServer {
				hub.RevokeServer(1)
			} else {
				hub.RevokeUserFromServer(2, 1)
			}
			if member.canAccessChannel(2) {
				t.Error("cached access survived revocation")
			}
			for {
				event, ok := member.PopOutgoing()
				if !ok {
					break
				}
				if event.Type == EventMessageCreated {
					t.Error("private content survived revocation")
				}
			}
		})
	}
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

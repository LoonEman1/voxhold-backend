package realtime

import (
	"context"
	"encoding/json"
	"testing"

	"voxhold-backend/internal/channel"
	channelsqlite "voxhold-backend/internal/channel/sqlite"
	"voxhold-backend/internal/message"
	messagesqlite "voxhold-backend/internal/message/sqlite"
	"voxhold-backend/internal/testdb"
)

func TestReactionBroadcastPersonalizesTabsAndPurgesRevokedQueues(t *testing.T) {
	db := testdb.Open(t)
	repo := messagesqlite.NewRepository(db)
	channels := channelsqlite.NewRepository(db)
	hub := NewHub()
	defer hub.Close()
	hub.SetChannelAccessLoader(channels.AccessibleChannelIDs)
	owner := NewClient(1, "owner", []int64{1})
	first := NewClient(2, "first", []int64{1})
	second := NewClient(2, "second", []int64{1})
	for _, client := range []*Client{owner, first, second} {
		if !hub.Register(client) || !hub.Subscribe(client, 1, 2) {
			t.Fatal("subscribe")
		}
		drainOutgoingEvents(client)
	}
	ctx := context.Background()
	publisher := NewMessageEventPublisher(hub)
	service := message.NewService(repo, publisher)
	value, err := service.Create(ctx, 1, 2, 1, message.CreateInput{Content: "message"})
	if err != nil {
		t.Fatal(err)
	}
	for _, client := range []*Client{owner, first, second} {
		drainOutgoingEvents(client)
	}
	if _, err = service.SetReaction(ctx, 1, 2, value.ID, 2, "❤️", true); err != nil {
		t.Fatal(err)
	}
	for _, client := range []*Client{owner, first, second} {
		event, ok := client.PopOutgoing()
		if !ok || event.Type != EventMessageReactionsChanged {
			t.Fatal("missing update")
		}
		data := event.Data.(message.ReactionsData)
		if len(data.Reactions) != 1 || data.Reactions[0].Me != (client.UserID() == 2) {
			t.Fatalf("wrong recipient selection: %#v", data)
		}
		encoded, shared, err := event.SharedJSON()
		var wire struct {
			Data message.ReactionsData `json:"data"`
		}
		if err != nil || !shared || json.Unmarshal(encoded, &wire) != nil || wire.Data.Reactions[0].Me != (client.UserID() == 2) {
			t.Fatalf("personalized encoding leaked across users: %s, %v", encoded, err)
		}
	}
	if _, err = service.SetReaction(ctx, 1, 2, value.ID, 2, "❤️", true); err != nil {
		t.Fatal(err)
	}
	assertNoOutgoingEvent(t, first)
	if _, err = service.SetReaction(ctx, 1, 2, value.ID, 1, "👍", true); err != nil {
		t.Fatal(err)
	}
	policy := channel.NewService(channels, NewChannelEventPublisher(hub))
	if _, err = policy.SetAccess(ctx, 1, 2, 1, channel.Access{Restricted: true}); err != nil {
		t.Fatal(err)
	}
	for {
		event, ok := first.PopOutgoing()
		if !ok {
			break
		}
		if event.Type == EventMessageReactionsChanged {
			t.Fatal("private reaction survived revocation")
		}
	}
}

func TestReactionQueueCoalescingKeepsTheNewestCommittedRevision(t *testing.T) {
	client := NewClient(1, "owner", nil)
	for _, revision := range []int64{7, 6, 8} {
		client.enqueue(OutgoingEvent{Type: EventMessageReactionsChanged, Data: message.ReactionsData{ChannelID: 2, MessageID: 1, Revision: revision, Reactions: []message.Reaction{}}})
	}
	if client.QueueStats().NormalPending != 1 {
		t.Fatal("updates were not bounded")
	}
	event, _ := client.PopOutgoing()
	if event.Data.(message.ReactionsData).Revision != 8 {
		t.Fatal("a delayed update replaced the latest state")
	}
}

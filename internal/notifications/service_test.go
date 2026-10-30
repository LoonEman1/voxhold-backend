package notifications

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"voxhold-backend/internal/channel"
	channelsqlite "voxhold-backend/internal/channel/sqlite"
	"voxhold-backend/internal/message"
	messagesqlite "voxhold-backend/internal/message/sqlite"
	"voxhold-backend/internal/testdb"
)

func TestDeliveryModesMentionsRolesAccessAndReplay(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	if _, err := db.Exec(`INSERT INTO users(id,username,password_hash) VALUES(4,'admin','hash'),(5,'Nikita','hash'); INSERT INTO server_members(server_id,user_id,role) VALUES(1,4,'admin'),(1,5,'member')`); err != nil {
		t.Fatal(err)
	}
	recipients := []int64{}
	events := []Event{}
	service := NewService(db, func(id int64) bool { return id >= 1 && id <= 5 }, func(id int64, event Event) {
		// Publishing may re-enter SQLite; no open rows/transaction may hold the pool.
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM users`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		recipients = append(recipients, id)
		events = append(events, event)
	}, nil)
	deliver := func(content string, want ...int64) {
		t.Helper()
		recipients = nil
		events = nil
		value, err := messagesqlite.NewRepository(db).Create(ctx, 1, 2, 1, content)
		if err != nil {
			t.Fatal(err)
		}
		if err := service.Deliver(value); err != nil {
			t.Fatal(err)
		}
		slices.Sort(recipients)
		slices.Sort(want)
		if !reflect.DeepEqual(recipients, want) {
			t.Fatalf("%q: got %v want %v", content, recipients, want)
		}
		for _, event := range events {
			if event.ServerID != 1 || event.ChannelID != 2 || event.ChannelName != "Text" || event.ServerName != "First" {
				t.Fatalf("metadata: %+v", event)
			}
		}
	}
	settings, err := service.Get(ctx, 1, 2, 2)
	if err != nil || settings.Mode != Mentions {
		t.Fatalf("default: %+v %v", settings, err)
	}
	deliver("hello")
	deliver("hello <@2>", 2)
	deliver("hello @Nikita", 5)
	deliver("<@&admin>", 4)
	deliver("<@&member>", 2, 5)
	deliver("mail@Nikita `@Nikita` \\@Nikita")
	if _, err := service.Set(ctx, 1, 2, 2, All); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Set(ctx, 1, 2, 5, None); err != nil {
		t.Fatal(err)
	}
	deliver("<@&member> @Nikita", 2)
	deliver("ordinary", 2)
	repo := channelsqlite.NewRepository(db)
	if _, err := repo.SetAccess(ctx, 1, 2, 1, channel.Access{Restricted: true}); err != nil {
		t.Fatal(err)
	}
	deliver("<@2> <@5> <@3>")
	deliver("<@&admin>", 4)
	if _, err := repo.SetAccess(ctx, 1, 2, 1, channel.Access{Restricted: true, UserIDs: []int64{2}}); err != nil {
		t.Fatal(err)
	}
	deliver("hello", 2)
	recipients = nil
	if err := service.Deliver(message.Message{ID: 10, ChannelID: 2, Replayed: true, Content: "<@2>"}); err != nil || len(recipients) != 0 {
		t.Fatalf("replay alerted: %v %v", recipients, err)
	}
	if err := service.Deliver(message.Message{ID: 11, ChannelID: 1, Content: "<@2>"}); err != nil || len(recipients) != 0 {
		t.Fatalf("voice alerted: %v %v", recipients, err)
	}
}

func TestSettingsCannotCrossScopeAndAreRemovedWithMembership(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	changed := []Settings{}
	s := NewService(db, nil, nil, func(id int64, value Settings) {
		if id != 2 {
			t.Fatalf("settings sent to %d", id)
		}
		changed = append(changed, value)
	})
	for _, input := range []struct {
		server, channel, user int64
		mode                  Mode
		err                   error
	}{
		{1, 2, 3, All, ErrForbidden}, {9, 2, 2, All, ErrForbidden}, {1, 999, 2, All, ErrForbidden},
		{1, 1, 2, All, ErrTextRequired}, {1, 2, 2, "invalid", ErrMode},
	} {
		if _, err := s.Set(ctx, input.server, input.channel, input.user, input.mode); !errors.Is(err, input.err) {
			t.Fatalf("%+v: %v", input, err)
		}
	}
	if _, err := s.Set(ctx, 1, 2, 2, None); err != nil {
		t.Fatal(err)
	}
	value, err := s.Get(ctx, 1, 2, 2)
	if err != nil || value.Mode != None || len(changed) != 1 {
		t.Fatalf("persist/sync: %+v %v %v", value, changed, err)
	}
	if _, err := db.Exec(`DELETE FROM server_members WHERE user_id=2 AND server_id=1; INSERT INTO server_members(server_id,user_id,role) VALUES(1,2,'member')`); err != nil {
		t.Fatal(err)
	}
	value, err = s.Get(ctx, 1, 2, 2)
	if err != nil || value.Mode != Mentions {
		t.Fatalf("new membership inherited settings: %+v %v", value, err)
	}
}

func TestIdempotentMessageRetryOnlyAlertsOnce(t *testing.T) {
	db := testdb.Open(t)
	repo := messagesqlite.NewRepository(db)
	ctx := context.Background()
	created, err := repo.CreateIdempotent(ctx, 1, 2, 1, "<@2>", "send-key")
	if err != nil {
		t.Fatal(err)
	}
	replay, err := repo.CreateIdempotent(ctx, 1, 2, 1, "<@2>", "send-key")
	if err != nil {
		t.Fatal(err)
	}
	if created.Replayed || !replay.Replayed || replay.ID != created.ID {
		t.Fatal("retry metadata")
	}
	count := 0
	s := NewService(db, func(id int64) bool { return id == 2 }, func(int64, Event) { count++ }, nil)
	if err := s.Deliver(created); err != nil {
		t.Fatal(err)
	}
	if err := s.Deliver(replay); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("alerts=%d", count)
	}
}

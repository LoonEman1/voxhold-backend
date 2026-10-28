package notifications_test

import (
	"context"
	"testing"
	"voxhold-backend/internal/channel"
	channelsqlite "voxhold-backend/internal/channel/sqlite"
	"voxhold-backend/internal/message"
	messagesqlite "voxhold-backend/internal/message/sqlite"
	"voxhold-backend/internal/notifications"
	readsqlite "voxhold-backend/internal/readstate/sqlite"
	"voxhold-backend/internal/testdb"
)

func TestDurableCountersUseNotificationRulesAndReadCursor(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	messages := messagesqlite.NewRepository(db)
	channels := channelsqlite.NewRepository(db)
	service := notifications.NewService(db, nil, nil, nil) // All recipients are offline.
	count := func(userID, channelID, want int64) {
		t.Helper()
		values, err := service.Counts(ctx, 1, userID)
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range values {
			if v.ChannelID == channelID {
				if v.UnreadCount != want {
					t.Fatalf("user %d count=%d want %d", userID, v.UnreadCount, want)
				}
				return
			}
		}
		t.Fatal("missing count")
	}
	send := func(content string) message.Message {
		t.Helper()
		value, err := messages.Create(ctx, 1, 2, 1, content)
		if err != nil {
			t.Fatal(err)
		}
		if err := service.Deliver(value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	send("plain text")
	count(2, 2, 0)
	first := send("hello <@2>")
	count(2, 2, 1)
	count(1, 2, 0)
	if _, err := service.Set(ctx, 1, 2, 2, notifications.All); err != nil {
		t.Fatal(err)
	}
	second := send("another")
	count(2, 2, 2)
	// The database, rather than a connected browser's memory, is authoritative.
	service = notifications.NewService(db, nil, nil, nil)
	count(2, 2, 2)
	if _, _, err := readsqlite.NewRepository(db).Mark(ctx, 1, 2, 2, first.ID); err != nil {
		t.Fatal(err)
	}
	count(2, 2, 1)
	if err := service.Deliver(first); err != nil {
		t.Fatal(err)
	}
	count(2, 2, 1)
	if _, err := messages.Delete(ctx, 1, 2, second.ID, 1); err != nil {
		t.Fatal(err)
	}
	count(2, 2, 0)
	if _, err := service.Set(ctx, 1, 2, 2, notifications.None); err != nil {
		t.Fatal(err)
	}
	send("<@2> muted")
	count(2, 2, 0)
	if _, err := service.Counts(ctx, 1, 3); err != notifications.ErrForbidden {
		t.Fatalf("outsider counters: %v", err)
	}
	if _, err := channels.SetAccess(ctx, 1, 2, 1, channel.Access{Restricted: true}); err != nil {
		t.Fatal(err)
	}
	values, err := service.Counts(ctx, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range values {
		if v.ChannelID == 2 {
			t.Fatal("private channel ID leaked")
		}
	}
}

func TestNewsNotificationsDefaultToPostsAndMentionedComments(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	messages := messagesqlite.NewRepository(db)
	channels := channelsqlite.NewRepository(db)
	service := notifications.NewService(db, nil, nil, nil)
	news, err := channels.Create(ctx, 1, 1, "News", channel.KindNews)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := service.Get(ctx, 1, news.ID, 2)
	if err != nil || settings.Mode != notifications.All {
		t.Fatalf("news defaults: %+v %v", settings, err)
	}
	post, err := messages.Create(ctx, 1, news.ID, 1, "News")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Deliver(post); err != nil {
		t.Fatal(err)
	}
	if _, err := channels.SetNews(ctx, 1, news.ID, 1, channel.NewsSettings{CommentsEnabled: true}); err != nil {
		t.Fatal(err)
	}
	comment, err := messages.CreateReferenced(ctx, 1, news.ID, 1, message.CreateInput{Content: "Discussion", ReplyToMessageID: &post.ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Deliver(comment); err != nil {
		t.Fatal(err)
	}
	mentioned, err := messages.CreateReferenced(ctx, 1, news.ID, 1, message.CreateInput{Content: "<@2> question", ReplyToMessageID: &post.ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Deliver(mentioned); err != nil {
		t.Fatal(err)
	}
	values, err := service.Counts(ctx, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range values {
		if v.ChannelID == news.ID && v.UnreadCount != 2 {
			t.Fatalf("discussion spam or missing post: %+v", v)
		}
	}
}

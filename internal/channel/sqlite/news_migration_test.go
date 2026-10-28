package sqlite_test

import (
	"context"
	"os"
	"testing"
	"voxhold-backend/internal/channel"
	channelsqlite "voxhold-backend/internal/channel/sqlite"
	"voxhold-backend/internal/message"
	messagesqlite "voxhold-backend/internal/message/sqlite"
	"voxhold-backend/internal/testdb"
)

func TestNewsMigrationRollbackPreservesPostsAndExistingACL(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	channels := channelsqlite.NewRepository(db)
	messages := messagesqlite.NewRepository(db)
	news, err := channels.Create(ctx, 1, 1, "News", channel.KindNews)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := channels.SetNews(ctx, 1, news.ID, 1, channel.NewsSettings{CommentsEnabled: true, PublisherIDs: []int64{2}}); err != nil {
		t.Fatal(err)
	}
	if _, err := channels.SetAccess(ctx, 1, news.ID, 1, channel.Access{Restricted: true, UserIDs: []int64{2}}); err != nil {
		t.Fatal(err)
	}
	post, err := messages.Create(ctx, 1, news.ID, 1, "Retained post")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := messages.CreateReferenced(ctx, 1, news.ID, 2, message.CreateInput{Content: "Removed on rollback", ReplyToMessageID: &post.ID}, nil); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"000026_notification_counters.down.sql", "000025_news_channels.down.sql", "000025_news_channels.up.sql", "000026_notification_counters.up.sql"} {
		data, err := os.ReadFile("../../../migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(data)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	var content, kind string
	var count int
	if err := db.QueryRow(`SELECT content FROM messages WHERE id=?`, post.ID).Scan(&content); err != nil || content != "Retained post" {
		t.Fatalf("post lost: %q %v", content, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM messages WHERE channel_id=?`, news.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("comment rollback: %d %v", count, err)
	}
	if err := db.QueryRow(`SELECT kind FROM channels WHERE id=?`, news.ID).Scan(&kind); err != nil || kind != "text" {
		t.Fatalf("storage kind: %s %v", kind, err)
	}
	if err := channels.CheckAccess(ctx, 1, news.ID, 2); err != nil {
		t.Fatalf("existing ACL lost: %v", err)
	}
	rows, err := db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("foreign key violation after rollback/re-upgrade")
	}
}

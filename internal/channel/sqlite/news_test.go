package sqlite_test

import (
	"context"
	"errors"
	"testing"

	"voxhold-backend/internal/channel"
	channelsqlite "voxhold-backend/internal/channel/sqlite"
	"voxhold-backend/internal/message"
	messagesqlite "voxhold-backend/internal/message/sqlite"
	"voxhold-backend/internal/testdb"
)

func TestNewsPermissionsThreadsAndPrivacy(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	if _, err := db.Exec(`INSERT INTO server_members(server_id,user_id,role) VALUES(1,3,'member')`); err != nil {
		t.Fatal(err)
	}
	channels := channelsqlite.NewRepository(db)
	messages := messagesqlite.NewRepository(db)
	news, err := channels.Create(ctx, 1, 1, "News", channel.KindNews)
	if err != nil {
		t.Fatal(err)
	}
	if news.Kind != channel.KindNews || news.CommentsEnabled || !news.CanPublish {
		t.Fatalf("created: %+v", news)
	}
	if _, err := channels.SetNews(ctx, 1, news.ID, 2, channel.NewsSettings{PublisherIDs: []int64{2}}); !errors.Is(err, channel.ErrForbidden) {
		t.Fatalf("reader changed settings: %v", err)
	}
	if _, err := channels.SetNews(ctx, 1, news.ID, 1, channel.NewsSettings{PublisherIDs: []int64{999}}); !errors.Is(err, channel.ErrAccessInvalid) {
		t.Fatalf("foreign publisher: %v", err)
	}
	if _, err := messages.Create(ctx, 1, news.ID, 2, "not an author"); !errors.Is(err, message.ErrForbidden) {
		t.Fatalf("reader posted: %v", err)
	}
	if _, err := channels.SetNews(ctx, 1, news.ID, 1, channel.NewsSettings{PublisherIDs: []int64{2}, CommentsEnabled: true}); err != nil {
		t.Fatal(err)
	}
	roster, err := channels.ListByServerID(ctx, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range roster {
		if c.ID == news.ID {
			found = c.Kind == channel.KindNews && c.CanPublish && c.CommentsEnabled
		}
	}
	if !found {
		t.Fatalf("publisher flags missing: %+v", roster)
	}
	post, err := messages.Create(ctx, 1, news.ID, 2, "A publication")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := messages.SetReaction(ctx, 1, news.ID, post.ID, 3, "👍", true); err != nil {
		t.Fatalf("reader reaction: %v", err)
	}
	input := message.CreateInput{Content: "Comment", ClientMessageID: "comment-key", ReplyToMessageID: &post.ID}
	first, err := messages.CreateReferenced(ctx, 1, news.ID, 3, input, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.ThreadRootID == nil || *first.ThreadRootID != post.ID {
		t.Fatalf("missing thread root: %+v", first)
	}
	replay, err := messages.CreateReferenced(ctx, 1, news.ID, 3, input, nil)
	if err != nil || !replay.Replayed || replay.ID != first.ID {
		t.Fatalf("comment retry: %+v %v", replay, err)
	}
	second, err := messages.CreateReferenced(ctx, 1, news.ID, 2, message.CreateInput{Content: "Reply to comment", ReplyToMessageID: &first.ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.ThreadRootID == nil || *second.ThreadRootID != post.ID {
		t.Fatal("nested comment escaped thread")
	}
	root, err := messages.GetPost(ctx, 1, news.ID, 3, post.ID)
	if err != nil || root.CommentCount != 2 {
		t.Fatalf("root count: %+v %v", root, err)
	}
	feed, err := messages.ListByChannelID(ctx, 1, news.ID, 3, nil, 50)
	if err != nil || len(feed) != 1 || feed[0].ID != post.ID {
		t.Fatalf("comments in feed: %+v %v", feed, err)
	}
	page, err := messages.ListComments(ctx, 1, news.ID, 3, post.ID, nil, 1)
	if err != nil || len(page) != 1 || page[0].ID != second.ID {
		t.Fatalf("comment page: %+v %v", page, err)
	}
	page, err = messages.ListComments(ctx, 1, news.ID, 3, post.ID, &second.ID, 10)
	if err != nil || len(page) != 1 || page[0].ID != first.ID {
		t.Fatalf("older comments: %+v %v", page, err)
	}
	if _, err := channels.SetNews(ctx, 1, news.ID, 1, channel.NewsSettings{}); err != nil {
		t.Fatal(err)
	}
	if _, err := messages.CreateReferenced(ctx, 1, news.ID, 3, message.CreateInput{Content: "disabled", ReplyToMessageID: &post.ID}, nil); !errors.Is(err, message.ErrForbidden) {
		t.Fatalf("disabled comment: %v", err)
	}
	if _, err := messages.Update(ctx, 1, news.ID, post.ID, 2, "revoked author edit"); !errors.Is(err, message.ErrForbidden) {
		t.Fatalf("revoked author edited: %v", err)
	}
	if _, err := messages.ListComments(ctx, 1, news.ID, 3, post.ID, nil, 50); err != nil {
		t.Fatalf("disabled history lost: %v", err)
	}
	if _, err := channels.SetNews(ctx, 1, news.ID, 1, channel.NewsSettings{PublisherIDs: []int64{2}, CommentsEnabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := channels.SetAccess(ctx, 1, news.ID, 1, channel.Access{Restricted: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := messages.Create(ctx, 1, news.ID, 2, "publisher without ACL"); !errors.Is(err, message.ErrForbidden) {
		t.Fatalf("publisher bypassed privacy: %v", err)
	}
	if _, err := messages.ListComments(ctx, 1, news.ID, 3, post.ID, nil, 50); !errors.Is(err, message.ErrForbidden) {
		t.Fatalf("private comments leaked: %v", err)
	}
	if _, _, err := messages.SetReaction(ctx, 1, news.ID, post.ID, 3, "👍", true); !errors.Is(err, message.ErrForbidden) {
		t.Fatalf("private reaction bypass: %v", err)
	}
	if _, err := channels.SetAccess(ctx, 1, news.ID, 1, channel.Access{}); err != nil {
		t.Fatal(err)
	}
	if _, err := messages.Delete(ctx, 1, news.ID, first.ID, 3); err != nil {
		t.Fatal(err)
	}
	root, err = messages.GetPost(ctx, 1, news.ID, 3, post.ID)
	if err != nil || root.CommentCount != 1 {
		t.Fatalf("delete count: %+v %v", root, err)
	}
	if _, err := messages.Delete(ctx, 1, news.ID, post.ID, 1); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := db.QueryRow(`SELECT count(*) FROM messages WHERE channel_id=?`, news.ID).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("orphaned comments: %d %v", remaining, err)
	}
}

func TestCommentCannotTargetAnotherChannel(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	channels := channelsqlite.NewRepository(db)
	messages := messagesqlite.NewRepository(db)
	news, err := channels.Create(ctx, 1, 1, "News", channel.KindNews)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := channels.SetNews(ctx, 1, news.ID, 1, channel.NewsSettings{CommentsEnabled: true}); err != nil {
		t.Fatal(err)
	}
	elsewhere, err := messages.Create(ctx, 1, 2, 1, "elsewhere")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := messages.CreateReferenced(ctx, 1, news.ID, 2, message.CreateInput{Content: "cross channel", ReplyToMessageID: &elsewhere.ID}, nil); !errors.Is(err, message.ErrReplyInvalid) {
		t.Fatalf("cross-channel comment: %v", err)
	}
	if _, err := messages.CreateReferenced(ctx, 1, news.ID, 2, message.CreateInput{}, &message.ForwardInput{SourceServerID: 1, SourceChannelID: 2, SourceMessageID: elsewhere.ID}); !errors.Is(err, message.ErrForbidden) {
		t.Fatalf("reader forwarded into feed: %v", err)
	}
}

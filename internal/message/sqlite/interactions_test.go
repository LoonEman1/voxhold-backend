package sqlite_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"voxhold-backend/internal/channel"
	channelsqlite "voxhold-backend/internal/channel/sqlite"
	"voxhold-backend/internal/message"
	messagesqlite "voxhold-backend/internal/message/sqlite"
	"voxhold-backend/internal/testdb"
)

func TestReactorPaginationLimitAndMigrationRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	repo := messagesqlite.NewRepository(db)
	value, err := repo.Create(ctx, 1, 2, 1, "keep this message")
	if err != nil {
		t.Fatal(err)
	}
	for id := int64(4); id <= 28; id++ {
		if _, err = db.Exec(`INSERT INTO users(id,username,password_hash) VALUES(?,?,'hash'); INSERT INTO server_members(server_id,user_id,role) VALUES(1,?,'member')`, id, fmt.Sprintf("reactor%d", id), id); err != nil {
			t.Fatal(err)
		}
		if _, _, err = repo.SetReaction(ctx, 1, 2, value.ID, id, "👍", true); err != nil {
			t.Fatal(err)
		}
	}
	first, err := repo.ListReactors(ctx, 1, 2, value.ID, 1, "👍", 0, 20)
	if err != nil || len(first.Users) != 20 || !first.HasMore || first.NextUserID == nil {
		t.Fatalf("first page: %#v %v", first, err)
	}
	second, err := repo.ListReactors(ctx, 1, 2, value.ID, 1, "👍", *first.NextUserID, 20)
	if err != nil || len(second.Users) != 5 || second.HasMore || second.Users[0].UserID <= first.Users[19].UserID {
		t.Fatalf("second page: %#v %v", second, err)
	}
	for _, emoji := range message.ReactionEmoji[:20] {
		if _, _, err = repo.SetReaction(ctx, 1, 2, value.ID, 1, emoji, true); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err = repo.SetReaction(ctx, 1, 2, value.ID, 1, message.ReactionEmoji[20], true); !errors.Is(err, message.ErrReactionLimit) {
		t.Fatalf("limit not enforced: %v", err)
	}
	if _, _, err = repo.SetReaction(ctx, 1, 2, value.ID, 2, "👍", true); err != nil {
		t.Fatalf("existing type must remain available: %v", err)
	}
	_, file, _, _ := runtime.Caller(0)
	for _, direction := range []string{"down", "up"} {
		sql, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "migrations", "000024_message_interactions."+direction+".sql"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(string(sql)); err != nil {
			t.Fatalf("migration %s: %v", direction, err)
		}
	}
	var content string
	if err = db.QueryRow(`SELECT content FROM messages WHERE id=?`, value.ID).Scan(&content); err != nil || content != "keep this message" {
		t.Fatalf("roundtrip lost message: %s %v", content, err)
	}
	rows, err := db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("foreign key violation after roundtrip")
	}
}

func TestRepliesSurviveEditsDeletionAndIdempotentRetries(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	repo := messagesqlite.NewRepository(db)
	original, err := repo.Create(ctx, 1, 2, 1, "original")
	if err != nil {
		t.Fatal(err)
	}
	input := message.CreateInput{Content: "answer", ClientMessageID: "reply-1", ReplyToMessageID: &original.ID}
	reply, err := repo.CreateReferenced(ctx, 1, 2, 2, input, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reply.Reply == nil || reply.Reply.Content != "original" || reply.Reply.Author.UserID != 1 {
		t.Fatalf("reply metadata: %#v", reply)
	}
	replayed, err := repo.CreateReferenced(ctx, 1, 2, 2, input, nil)
	if err != nil || !replayed.Replayed || replayed.ID != reply.ID {
		t.Fatalf("retry: %#v %v", replayed, err)
	}
	if _, err = repo.CreateIdempotent(ctx, 1, 2, 2, "answer", "reply-1"); !errors.Is(err, message.ErrIdempotencyConflict) {
		t.Fatalf("request kind changed: %v", err)
	}
	if _, err = repo.CreateReferenced(ctx, 1, 4, 2, input, nil); !errors.Is(err, message.ErrReplyInvalid) {
		t.Fatalf("cross-channel reply accepted: %v", err)
	}
	before, err := repo.Sync(ctx, 1, 2, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Update(ctx, 1, 2, original.ID, 1, "edited original"); err != nil {
		t.Fatal(err)
	}
	delta, err := repo.Sync(ctx, 1, 2, 2, &before.Cursor)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, value := range delta.Messages {
		if value.ID == reply.ID {
			found = value.Reply != nil && value.Reply.Content == "edited original" && value.Revision > reply.Revision
		}
	}
	if !found {
		t.Fatal("editing a reply target did not update the durable journal")
	}
	if _, err = repo.Delete(ctx, 1, 2, original.ID, 1); err != nil {
		t.Fatal(err)
	}
	delta, err = repo.Sync(ctx, 1, 2, 2, &delta.Cursor)
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for _, value := range delta.Messages {
		if value.ID == reply.ID {
			found = value.Reply != nil && value.Reply.Deleted && value.Reply.Content == "" && value.Reply.MessageID == original.ID
		}
	}
	if !found {
		t.Fatal("deleted target remained in the reply quote")
	}
}

func TestForwardChecksBothChannelsAndKeepsAnImmutableSnapshot(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	repo := messagesqlite.NewRepository(db)
	channels := channelsqlite.NewRepository(db)
	original, err := repo.Create(ctx, 1, 2, 1, "hello <@2>")
	if err != nil {
		t.Fatal(err)
	}
	input := message.ForwardInput{SourceServerID: 1, SourceChannelID: 2, SourceMessageID: original.ID, ClientMessageID: "forward-1"}
	create := message.CreateInput{ClientMessageID: input.ClientMessageID}
	if _, err = channels.SetAccess(ctx, 1, 2, 1, channel.Access{Restricted: true}); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.CreateReferenced(ctx, 1, 4, 2, create, &input); !errors.Is(err, message.ErrForbidden) {
		t.Fatalf("private source forwarded: %v", err)
	}
	if _, err = channels.SetAccess(ctx, 1, 2, 1, channel.Access{Restricted: true, UserIDs: []int64{2}}); err != nil {
		t.Fatal(err)
	}
	if _, err = channels.SetAccess(ctx, 1, 4, 1, channel.Access{Restricted: true}); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.CreateReferenced(ctx, 1, 4, 2, create, &input); !errors.Is(err, message.ErrForbidden) {
		t.Fatalf("private destination accepted: %v", err)
	}
	if _, err = channels.SetAccess(ctx, 1, 4, 1, channel.Access{}); err != nil {
		t.Fatal(err)
	}
	forward, err := repo.CreateReferenced(ctx, 1, 4, 2, create, &input)
	if err != nil {
		t.Fatal(err)
	}
	if forward.Forward == nil || forward.Forward.Author.UserID != 1 || forward.Author.UserID != 2 || forward.Forward.MentionNames[2] != "member" {
		t.Fatalf("forward: %#v", forward)
	}
	if _, err = repo.Update(ctx, 1, 4, forward.ID, 2, "forged attribution"); !errors.Is(err, message.ErrForwardEdit) {
		t.Fatalf("immutable snapshot edited: %v", err)
	}
	if _, err = repo.Update(ctx, 1, 2, original.ID, 1, "later edit"); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Delete(ctx, 1, 2, original.ID, 1); err != nil {
		t.Fatal(err)
	}
	replayed, err := repo.CreateReferenced(ctx, 1, 4, 2, create, &input)
	if err != nil || !replayed.Replayed || replayed.ID != forward.ID || replayed.Content != "hello <@2>" {
		t.Fatalf("forward retry changed its snapshot: %#v %v", replayed, err)
	}
	input.SourceChannelID = 4
	input.SourceMessageID = forward.ID
	create.ClientMessageID = "forward-2"
	second, err := repo.CreateReferenced(ctx, 1, 2, 2, create, &input)
	if err != nil || second.Forward == nil || second.Forward.Author.UserID != 1 {
		t.Fatalf("forward chain lost original attribution: %#v %v", second, err)
	}
}

func TestReactionPersonalizationEveryReadPathAndMembershipCleanup(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	repo := messagesqlite.NewRepository(db)
	value, err := repo.Create(ctx, 1, 2, 1, "searchable original")
	if err != nil {
		t.Fatal(err)
	}
	state, changed, err := repo.SetReaction(ctx, 1, 2, value.ID, 2, "❤️", true)
	if err != nil || !changed {
		t.Fatalf("reaction: %v %v", changed, err)
	}
	if !state.ForUser(2).Reactions[0].Me || state.ForUser(1).Reactions[0].Me {
		t.Fatal("recipient selections were mixed")
	}
	repeated, changed, err := repo.SetReaction(ctx, 1, 2, value.ID, 2, "❤️", true)
	if err != nil || changed || repeated.Revision != state.Revision {
		t.Fatalf("non-idempotent PUT: %v %v", changed, err)
	}
	check := func(value message.Message, mine bool) {
		t.Helper()
		if len(value.Reactions) != 1 || value.Reactions[0].Emoji != "❤️" || value.Reactions[0].Count != 1 || value.Reactions[0].Me != mine || value.ReactionsRevision < state.Revision {
			t.Fatalf("missing/personalized reactions: %#v", value)
		}
	}
	list, err := repo.ListByChannelID(ctx, 1, 2, 2, nil, 50)
	if err != nil {
		t.Fatal(err)
	}
	check(list[0], true)
	page, err := repo.Sync(ctx, 1, 2, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	check(page.Messages[0], false)
	results, err := repo.Search(ctx, 1, 2, "searchable", nil, 20)
	if err != nil {
		t.Fatal(err)
	}
	check(results[0].Message, true)
	context, err := repo.GetContext(ctx, 1, 2, value.ID, 2, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	check(context.Messages[context.TargetIndex], true)
	pin, _, err := repo.Pin(ctx, 1, 2, value.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	check(pin.Message, false)
	pins, err := repo.ListPinned(ctx, 1, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	check(pins[0].Message, true)
	if _, _, err = repo.SetReaction(ctx, 1, 2, value.ID, 3, "❤️", true); !errors.Is(err, message.ErrForbidden) {
		t.Fatalf("outsider reacted: %v", err)
	}
	if _, err = db.Exec(`DELETE FROM server_members WHERE server_id=1 AND user_id=2`); err != nil {
		t.Fatal(err)
	}
	delta, err := repo.Sync(ctx, 1, 2, 1, &page.Cursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(delta.Messages) == 0 || len(delta.Messages[0].Reactions) != 0 || delta.Messages[0].Revision <= state.Revision {
		t.Fatal("membership cascade did not journal removal")
	}
}

func TestConcurrentReactionRetriesHaveOneRowAndOneRevision(t *testing.T) {
	ctx := context.Background()
	db := testdb.OpenFile(t)
	repo := messagesqlite.NewRepository(db)
	value, err := repo.Create(ctx, 1, 2, 1, "original")
	if err != nil {
		t.Fatal(err)
	}
	errors := make(chan error, 24)
	var wg sync.WaitGroup
	for range 24 {
		wg.Go(func() { _, _, err := repo.SetReaction(ctx, 1, 2, value.ID, 2, "👍", true); errors <- err })
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int
	var revision int64
	if err = db.QueryRow(`SELECT COUNT(*) FROM message_reactions WHERE message_id=?`, value.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT revision FROM messages WHERE id=?`, value.ID).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if count != 1 || revision != value.Revision+1 {
		t.Fatalf("concurrent retry duplicated a reaction: count=%d revision=%d", count, revision)
	}
}

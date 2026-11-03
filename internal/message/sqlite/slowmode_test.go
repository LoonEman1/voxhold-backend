package sqlite_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"voxhold-backend/internal/message"
	messagesqlite "voxhold-backend/internal/message/sqlite"
	"voxhold-backend/internal/testdb"
)

func TestSlowmodeSerializesConcurrentSends(t *testing.T) {
	db := testdb.OpenFile(t)
	if _, err := db.Exec(`UPDATE channels SET slowmode_seconds=60 WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	repo := messagesqlite.NewRepository(db)
	start := make(chan struct{})
	results := make(chan error, 12)
	var workers sync.WaitGroup
	for i := range 12 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			_, err := repo.CreateIdempotent(context.Background(), 1, 2, 2, "hello", fmt.Sprintf("concurrent-%d", i))
			results <- err
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	var sent, delayed int
	for err := range results {
		if err == nil {
			sent++
		} else if _, ok := message.IsSlowmode(err); ok {
			delayed++
		} else {
			t.Fatalf("unexpected send error: %v", err)
		}
	}
	if sent != 1 || delayed != 11 {
		t.Fatalf("sent=%d delayed=%d, want 1 and 11", sent, delayed)
	}
}

type slowmodeEvents struct{}

func (slowmodeEvents) PublishMessageCreated(message.Message)      {}
func (slowmodeEvents) PublishMessageUpdated(message.Message)      {}
func (slowmodeEvents) PublishMessageDeleted(message.Message)      {}
func (slowmodeEvents) PublishMessagePinned(message.PinnedMessage) {}
func (slowmodeEvents) PublishMessageUnpinned(int64, int64)        {}

func TestSlowmodeAllowsCommittedRetryAndRejectsNewRequest(t *testing.T) {
	db := testdb.Open(t)
	if _, err := db.Exec(`UPDATE channels SET slowmode_seconds=60 WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	service := message.NewService(messagesqlite.NewRepository(db), slowmodeEvents{})
	input := message.CreateInput{Content: "hello", ClientMessageID: "stable-send"}
	first, err := service.Create(context.Background(), 1, 2, 2, input)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := service.Create(context.Background(), 1, 2, 2, input)
	if err != nil || !retry.Replayed || retry.ID != first.ID {
		t.Fatalf("committed retry: %+v err=%v", retry, err)
	}
	input.ClientMessageID = "new-send"
	if _, err = service.Create(context.Background(), 1, 2, 2, input); err == nil {
		t.Fatal("new request bypassed slowmode")
	} else if _, ok := message.IsSlowmode(err); !ok {
		t.Fatalf("want slowmode error, got %v", err)
	}
}

func TestSlowmodeDelaysMembersButNotAdmins(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	if _, err := db.Exec(`INSERT INTO users(id,username,password_hash) VALUES(21,'slowmember','h');
 INSERT INTO server_members(server_id,user_id,role) VALUES(1,21,'member');
 UPDATE channels SET slowmode_seconds=60 WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	repo := messagesqlite.NewRepository(db)
	if wait, err := repo.CheckSlowmode(ctx, 1, 2, 21); err != nil || wait != 0 {
		t.Fatalf("first message wait=%v err=%v", wait, err)
	}
	if _, err := repo.Create(ctx, 1, 2, 21, "hello"); err != nil {
		t.Fatal(err)
	}
	wait, err := repo.CheckSlowmode(ctx, 1, 2, 21)
	if err != nil || wait <= 0 || wait > 60*1e9 {
		t.Fatalf("member second message wait=%v err=%v", wait, err)
	}
	if _, err := repo.Create(ctx, 1, 2, 1, "admin"); err != nil {
		t.Fatal(err)
	}
	if wait, err := repo.CheckSlowmode(ctx, 1, 2, 1); err != nil || wait != 0 {
		t.Fatalf("owner must not be delayed: wait=%v err=%v", wait, err)
	}
}

func TestSlowmodeLookupUsesTheAuthorIndex(t *testing.T) {
	db := testdb.Open(t)
	var plan string
	rows, err := db.Query(`EXPLAIN QUERY PLAN SELECT created_at FROM messages WHERE channel_id=? AND author_user_id=? ORDER BY id DESC LIMIT 1`, 2, 21)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan += detail + "; "
	}
	if !strings.Contains(plan, "idx_messages_channel_author") {
		t.Fatalf("slowmode lookup does not use the author index: %s", plan)
	}
}

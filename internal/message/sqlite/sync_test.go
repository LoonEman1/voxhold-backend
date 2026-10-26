package sqlite_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"voxhold-backend/internal/message"
	messagesqlite "voxhold-backend/internal/message/sqlite"
	"voxhold-backend/internal/testdb"
)

func TestSendKeyIsAtomicScopedAndSurvivesDeletion(t *testing.T) {
	db := testdb.OpenFile(t)
	repo := messagesqlite.NewRepository(db)
	ctx := context.Background()
	type result struct {
		value message.Message
		err   error
	}
	results := make(chan result, 8)
	for i := 0; i < 8; i++ {
		go func() {
			value, err := repo.CreateIdempotent(ctx, 1, 2, 1, "original", "retry-key")
			results <- result{value, err}
		}()
	}
	var id int64
	for i := 0; i < 8; i++ {
		got := <-results
		if got.err != nil {
			t.Fatal(got.err)
		}
		if id == 0 {
			id = got.value.ID
		}
		if got.value.ID != id {
			t.Fatal("retry created a duplicate", got.value.ID, id)
		}
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM messages").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if _, err := repo.CreateIdempotent(ctx, 1, 2, 1, "changed", "retry-key"); !errors.Is(err, message.ErrIdempotencyConflict) {
		t.Fatal(err)
	}
	if _, err := repo.CreateIdempotent(ctx, 1, 2, 3, "original", "retry-key"); !errors.Is(err, message.ErrForbidden) {
		t.Fatal(err)
	}
	other, err := repo.CreateIdempotent(ctx, 1, 2, 2, "original", "retry-key")
	if err != nil || other.ID == id {
		t.Fatal(other, err)
	}
	if _, err := repo.Delete(ctx, 1, 2, id, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateIdempotent(ctx, 1, 2, 1, "original", "retry-key"); !errors.Is(err, message.ErrMessageNotFound) {
		t.Fatal("deleted message was resurrected", err)
	}
}

func TestSyncReplaysEditsDeletesAndMultiplePages(t *testing.T) {
	db := testdb.Open(t)
	repo := messagesqlite.NewRepository(db)
	ctx := context.Background()
	base, err := repo.Sync(ctx, 1, 2, 1, nil)
	if err != nil || !base.Reset || len(base.Messages) != 0 {
		t.Fatal(base, err)
	}
	var first int64
	for i := 0; i < 230; i++ {
		value, err := repo.Create(ctx, 1, 2, 1, fmt.Sprintf("message %d", i))
		if err != nil {
			t.Fatal(err)
		}
		if first == 0 {
			first = value.ID
		}
	}
	if _, err := repo.Update(ctx, 1, 2, first, 1, "edited"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Delete(ctx, 1, 2, first+1, 1); err != nil {
		t.Fatal(err)
	}
	cursor := base.Cursor
	seen := map[int64]message.Message{}
	deleted := map[int64]bool{}
	pages := 0
	for {
		page, err := repo.Sync(ctx, 1, 2, 2, &cursor)
		if err != nil || page.Reset || page.Cursor <= cursor {
			t.Fatal(page, err)
		}
		pages++
		for _, value := range page.Messages {
			seen[value.ID] = value
		}
		for _, id := range page.DeletedIDs {
			deleted[id] = true
			delete(seen, id)
		}
		cursor = page.Cursor
		if !page.HasMore {
			break
		}
	}
	if pages != 2 || len(seen) != 229 || seen[first].Content != "edited" || seen[first].Revision != 2 || !deleted[first+1] {
		t.Fatal(pages, len(seen), seen[first], deleted)
	}
	if _, err := repo.Sync(ctx, 1, 2, 3, &cursor); !errors.Is(err, message.ErrForbidden) {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE message_changes SET created_at = 0"); err != nil {
		t.Fatal(err)
	}
	if err := repo.PruneChanges(ctx); err != nil {
		t.Fatal(err)
	}
	reset, err := repo.Sync(ctx, 1, 2, 1, &base.Cursor)
	if err != nil || !reset.Reset || len(reset.Messages) != 50 || !reset.HasOlder || reset.Cursor != cursor {
		t.Fatal(reset, err)
	}
	// A channel cascade must not make the delete journal's FK fail.
	if _, err := db.Exec("DELETE FROM channels WHERE id = 2"); err != nil {
		t.Fatal(err)
	}
}

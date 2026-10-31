package notifications

import (
	"context"
	"testing"
	"voxhold-backend/internal/channel"
	channelsqlite "voxhold-backend/internal/channel/sqlite"
	"voxhold-backend/internal/message"
	messagesqlite "voxhold-backend/internal/message/sqlite"
	"voxhold-backend/internal/storage"
	"voxhold-backend/internal/testdb"
)

func TestWorkerPreservesQueueOrderAndDrainsOnStop(t *testing.T) {
	db := testdb.Open(t)
	repo := messagesqlite.NewRepository(db)
	events := make(chan int64, 3)
	s := NewService(db, func(id int64) bool { return id == 2 }, func(_ int64, event Event) { events <- event.Message.ID }, nil)
	var ids []int64
	for range 2 {
		value, err := repo.Create(context.Background(), 1, 2, 1, "<@2>")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, value.ID)
		s.Enqueue(value)
	}
	if len(events) != 0 {
		t.Fatal("enqueue performed delivery synchronously below capacity")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.Run(ctx)
	for _, id := range ids {
		select {
		case got := <-events:
			if got != id {
				t.Fatalf("order: %d want %d", got, id)
			}
		default:
			t.Fatal("lost queued notification")
		}
	}
}

func TestFullQueueFallsBackWithoutDroppingDelivery(t *testing.T) {
	db := testdb.Open(t)
	value, err := messagesqlite.NewRepository(db).Create(context.Background(), 1, 2, 1, "<@2>")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	s := NewService(db, func(id int64) bool { return id == 2 }, func(int64, Event) { count++ }, nil)
	s.queue = make(chan message.Message, 1)
	s.queue <- message.Message{Replayed: true}
	s.Enqueue(value)
	if count != 1 {
		t.Fatal("saturated queue dropped the notification")
	}
}

func TestNoRecipientsDoesNotWaitForSQLiteWriter(t *testing.T) {
	db := testdb.OpenFile(t)
	value, err := messagesqlite.NewRepository(db).Create(context.Background(), 1, 2, 1, "plain text")
	if err != nil {
		t.Fatal(err)
	}
	writer, err := storage.BeginWrite(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Rollback()
	if err := NewService(db, nil, nil, nil).Deliver(value); err != nil {
		t.Fatalf("read-only delivery waited for writer: %v", err)
	}
}

func TestInsertRechecksAccessAndOnlyInsertedRowsPublish(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	value, err := messagesqlite.NewRepository(db).Create(ctx, 1, 2, 1, "<@2>")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	s := NewService(db, func(id int64) bool { return id == 2 }, func(int64, Event) { count++ }, nil)
	// Model access being revoked after candidate selection but before insertion.
	if _, err := channelsqlite.NewRepository(db).SetAccess(ctx, 1, 2, 1, channel.Access{Restricted: true}); err != nil {
		t.Fatal(err)
	}
	inserted, err := s.persistDeliveries(ctx, 2, value.ID, []int64{2})
	if err != nil || len(inserted) != 0 {
		t.Fatalf("revoked recipient inserted: %v %v", inserted, err)
	}
	if err := s.Deliver(value); err != nil || count != 0 {
		t.Fatalf("phantom event: %d %v", count, err)
	}
	if _, err := channelsqlite.NewRepository(db).SetAccess(ctx, 1, 2, 1, channel.Access{Restricted: false}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.Deliver(value); err != nil {
			t.Fatal(err)
		}
	}
	if count != 1 {
		t.Fatalf("duplicate notifications: %d", count)
	}
}

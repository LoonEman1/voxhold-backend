package sqlite_test

import (
	"context"
	"testing"

	messagesqlite "voxhold-backend/internal/message/sqlite"
	"voxhold-backend/internal/testdb"
)

func TestConcurrentCreatesUseProductionWALPool(t *testing.T) {
	repo := messagesqlite.NewRepository(testdb.OpenFile(t))
	start := make(chan struct{})
	errors := make(chan error, 160)
	for worker := 0; worker < 8; worker++ {
		go func() {
			<-start
			for i := 0; i < 20; i++ {
				_, err := repo.Create(context.Background(), 1, 2, 1, "concurrent message")
				errors <- err
			}
		}()
	}
	close(start)
	for i := 0; i < 160; i++ {
		if err := <-errors; err != nil {
			t.Errorf("create: %v", err)
		}
	}
}

func TestMessageRevisionSurvivesEveryReadPath(t *testing.T) {
	repo := messagesqlite.NewRepository(testdb.Open(t))
	ctx := context.Background()
	created, err := repo.Create(ctx, 1, 2, 1, "original")
	if err != nil {
		t.Fatal(err)
	}
	first, err := repo.Update(ctx, 1, 2, created.ID, 1, "first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.Update(ctx, 1, 2, created.ID, 1, "second")
	if err != nil {
		t.Fatal(err)
	}
	if created.Revision != 1 || first.Revision != 2 || second.Revision != 3 {
		t.Fatal(created.Revision, first.Revision, second.Revision)
	}
	page, err := repo.ListByChannelID(ctx, 1, 2, 1, nil, 10)
	if err != nil || len(page) != 1 || page[0].Revision != 3 {
		t.Fatal(page, err)
	}
	pin, _, err := repo.Pin(ctx, 1, 2, created.ID, 1)
	if err != nil || pin.Message.Revision != 3 {
		t.Fatal(pin, err)
	}
	pins, err := repo.ListPinned(ctx, 1, 2, 1)
	if err != nil || len(pins) != 1 || pins[0].Message.Revision != 3 {
		t.Fatal(pins, err)
	}
	results, err := repo.Search(ctx, 1, 1, "second", nil, 10)
	if err != nil || len(results) != 1 || results[0].Message.Revision != 3 {
		t.Fatal(results, err)
	}
	window, err := repo.GetContext(ctx, 1, 2, created.ID, 1, 1, 1)
	if err != nil || len(window.Messages) != 1 || window.Messages[0].Revision != 3 {
		t.Fatal(window, err)
	}
}

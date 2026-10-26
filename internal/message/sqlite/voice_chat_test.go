package sqlite_test

import (
	"context"
	"errors"
	"testing"
	"voxhold-backend/internal/message"
	messagesqlite "voxhold-backend/internal/message/sqlite"
	"voxhold-backend/internal/readstate"
	readsqlite "voxhold-backend/internal/readstate/sqlite"
	"voxhold-backend/internal/testdb"
)

func TestVoiceChatUsesHistorySearchAndReadACL(t *testing.T) {
	db := testdb.Open(t)
	repo := messagesqlite.NewRepository(db)
	reads := readsqlite.NewRepository(db)
	ctx := context.Background()
	created, err := repo.Create(ctx, 1, 1, 1, "voiceneedle message")
	if err != nil {
		t.Fatal(err)
	}
	history, err := repo.ListByChannelID(ctx, 1, 1, 2, nil, 50)
	if err != nil || len(history) != 1 || history[0].ID != created.ID {
		t.Fatal(history, err)
	}
	result, err := repo.Search(ctx, 1, 2, "voiceneedle", nil, 25)
	if err != nil || len(result) != 1 || result[0].Message.ID != created.ID {
		t.Fatal(result, err)
	}
	if _, _, err = reads.Mark(ctx, 1, 1, 2, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Create(ctx, 1, 1, 3, "blocked"); !errors.Is(err, message.ErrForbidden) {
		t.Fatal(err)
	}
	if _, err = repo.ListByChannelID(ctx, 1, 1, 3, nil, 50); !errors.Is(err, message.ErrForbidden) {
		t.Fatal(err)
	}
	if _, _, err = reads.Mark(ctx, 1, 1, 3, created.ID); !errors.Is(err, readstate.ErrForbidden) {
		t.Fatal(err)
	}
	if _, err = repo.Create(ctx, 1, 3, 1, "cross-server"); !errors.Is(err, message.ErrChannelNotFound) {
		t.Fatal(err)
	}
}

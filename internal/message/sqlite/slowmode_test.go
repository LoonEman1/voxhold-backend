package sqlite_test

import (
	"context"
	"testing"

	messagesqlite "voxhold-backend/internal/message/sqlite"
	"voxhold-backend/internal/testdb"
)

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

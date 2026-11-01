package sqlite_test

import (
	"context"
	"strings"
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

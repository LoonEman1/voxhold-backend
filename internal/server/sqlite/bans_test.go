package sqlite_test

import (
	"context"
	"errors"
	"testing"

	"voxhold-backend/internal/server"
	serversqlite "voxhold-backend/internal/server/sqlite"
	"voxhold-backend/internal/testdb"
)

func TestBanReasonListAndUnbanPermissions(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	if _, err := db.Exec(`INSERT INTO users(id,username,password_hash) VALUES(12,'adminA','h'),(13,'adminB','h'),(14,'victim','h'),(15,'victim2','h');
 INSERT INTO server_members(server_id,user_id,role) VALUES(1,12,'admin'),(1,13,'admin'),(1,14,'member'),(1,15,'member')`); err != nil {
		t.Fatal(err)
	}
	repo := serversqlite.NewRepository(db)
	if err := repo.BanMemberWithReason(ctx, 1, 12, 14, "spam"); err != nil {
		t.Fatal(err)
	}
	if err := repo.BanMember(ctx, 1, 1, 15); err != nil {
		t.Fatal(err)
	}
	bans, err := repo.ListBans(ctx, 1, 13)
	if err != nil || len(bans) != 2 {
		t.Fatalf("bans=%v err=%v", bans, err)
	}
	reasons := map[int64]string{}
	for _, b := range bans {
		reasons[b.UserID] = b.Reason
	}
	if reasons[14] != "spam" || reasons[15] != "" {
		t.Fatalf("reasons=%v", reasons)
	}
	if _, err = repo.ListBans(ctx, 1, 14); !errors.Is(err, server.ErrManageMembersForbidden) {
		t.Fatalf("banned user listing err=%v", err)
	}
	if err = repo.Unban(ctx, 1, 13, 14); !errors.Is(err, server.ErrManageMembersForbidden) {
		t.Fatalf("admin lifting someone else's ban err=%v", err)
	}
	if err = repo.Unban(ctx, 1, 12, 14); err != nil {
		t.Fatalf("admin lifting own ban: %v", err)
	}
	if err = repo.Unban(ctx, 1, 1, 15); err != nil {
		t.Fatalf("owner lifting ban: %v", err)
	}
	if err = repo.Unban(ctx, 1, 1, 15); !errors.Is(err, server.ErrBanNotFound) {
		t.Fatalf("second unban err=%v", err)
	}
}

package sqlite_test

import (
	"context"
	"errors"
	"testing"
	"time"

	messagesqlite "voxhold-backend/internal/message/sqlite"
	"voxhold-backend/internal/permissions"
	"voxhold-backend/internal/server"
	serversqlite "voxhold-backend/internal/server/sqlite"
	"voxhold-backend/internal/testdb"
)

func TestMemberTimeoutRemovesPublishUntilItEnds(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	if _, err := db.Exec(`INSERT INTO users(id,username,password_hash) VALUES(31,'tadmin','h'),(32,'tmember','h'),(33,'tother','h');
 INSERT INTO server_members(server_id,user_id,role) VALUES(1,31,'admin'),(1,32,'member'),(1,33,'admin')`); err != nil {
		t.Fatal(err)
	}
	repo := serversqlite.NewRepository(db)
	until := time.Now().Add(time.Hour).Unix()
	if err := repo.SetMemberTimeout(ctx, 1, 31, 33, &until); !errors.Is(err, server.ErrManageMembersForbidden) {
		t.Fatalf("admin timing out another admin: %v", err)
	}
	if err := repo.SetMemberTimeout(ctx, 1, 31, 32, &until); err != nil {
		t.Fatal(err)
	}
	for bit, name := range map[int]string{permissions.Publish: "publish", permissions.Voice: "voice", permissions.Stream: "stream"} {
		if ok, err := permissions.Allowed(ctx, db, 1, 32, bit); err != nil || ok {
			t.Fatalf("%s allowed during timeout: %v %v", name, ok, err)
		}
	}
	if ok, _ := permissions.Allowed(ctx, db, 1, 32, permissions.Read); !ok {
		t.Fatal("read must survive a timeout")
	}
	if _, err := messagesqlite.NewRepository(db).Create(ctx, 1, 2, 32, "hi"); err == nil {
		t.Fatal("timed-out member posted a message")
	}
	if err := repo.SetMemberTimeout(ctx, 1, 31, 32, nil); err != nil {
		t.Fatal(err)
	}
	if ok, _ := permissions.Allowed(ctx, db, 1, 32, permissions.Publish); !ok {
		t.Fatal("publish not restored after clearing the timeout")
	}
}

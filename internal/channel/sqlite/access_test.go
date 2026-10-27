package sqlite_test

import (
	"context"
	"errors"
	"testing"

	"voxhold-backend/internal/channel"
	channelsqlite "voxhold-backend/internal/channel/sqlite"
	"voxhold-backend/internal/message"
	messagesqlite "voxhold-backend/internal/message/sqlite"
	"voxhold-backend/internal/readstate"
	readsqlite "voxhold-backend/internal/readstate/sqlite"
	"voxhold-backend/internal/server"
	"voxhold-backend/internal/testdb"
)

func TestRestrictedChannelPolicyCoversEveryMessagePath(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	channels := channelsqlite.NewRepository(db)
	messages := messagesqlite.NewRepository(db)
	reads := readsqlite.NewRepository(db)
	created, err := messages.Create(ctx, 1, 2, 2, "secret needle")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := reads.Mark(ctx, 1, 2, 2, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := messages.Pin(ctx, 1, 2, created.ID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := channels.SetAccess(ctx, 1, 2, 1, channel.Access{Restricted: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := channels.GetAccess(ctx, 1, 2, 2); !errors.Is(err, channel.ErrForbidden) {
		t.Fatalf("member read ACL: %v", err)
	}
	if _, err := channels.SetAccess(ctx, 1, 2, 2, channel.Access{}); !errors.Is(err, channel.ErrForbidden) {
		t.Fatalf("member changed ACL: %v", err)
	}
	list, err := channels.ListByServerID(ctx, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range list {
		if c.ID == 2 {
			t.Fatal("private channel leaked")
		}
	}
	if err := channels.CheckAccess(ctx, 1, 2, 2); !errors.Is(err, channel.ErrForbidden) {
		t.Fatalf("access: %v", err)
	}
	if _, err := channels.ListMemberIDs(ctx, 1, 2, 2); !errors.Is(err, channel.ErrForbidden) {
		t.Fatalf("roster: %v", err)
	}
	if _, err := messages.CreateIdempotent(ctx, 1, 2, 2, "secret needle", "key"); !errors.Is(err, message.ErrForbidden) {
		t.Fatalf("send: %v", err)
	}
	if _, err := messages.ListByChannelID(ctx, 1, 2, 2, nil, 50); !errors.Is(err, message.ErrForbidden) {
		t.Fatalf("history: %v", err)
	}
	if _, err := messages.Sync(ctx, 1, 2, 2, nil); !errors.Is(err, message.ErrForbidden) {
		t.Fatalf("sync: %v", err)
	}
	if _, err := messages.GetContext(ctx, 1, 2, created.ID, 2, 10, 10); !errors.Is(err, message.ErrForbidden) {
		t.Fatalf("context: %v", err)
	}
	if _, err := messages.ListPinned(ctx, 1, 2, 2); !errors.Is(err, message.ErrForbidden) {
		t.Fatalf("pins: %v", err)
	}
	if _, err := messages.Update(ctx, 1, 2, created.ID, 2, "changed"); !errors.Is(err, message.ErrForbidden) {
		t.Fatalf("edit: %v", err)
	}
	if _, err := messages.Delete(ctx, 1, 2, created.ID, 2); !errors.Is(err, message.ErrForbidden) {
		t.Fatalf("delete: %v", err)
	}
	if _, err := messages.Unpin(ctx, 1, 2, created.ID, 2); err == nil {
		t.Fatalf("unpin: %v", err)
	}
	found, err := messages.Search(ctx, 1, 2, "needle", nil, 50)
	if err != nil || len(found) != 0 {
		t.Fatalf("search leaked: %v %v", found, err)
	}
	if _, _, err := reads.Mark(ctx, 1, 2, 2, created.ID); !errors.Is(err, readstate.ErrForbidden) {
		t.Fatalf("mark: %v", err)
	}
	if _, err := reads.ListByChannelID(ctx, 1, 2, 2); !errors.Is(err, readstate.ErrForbidden) {
		t.Fatalf("read snapshot: %v", err)
	}
	ownReads, err := reads.ListByUserID(ctx, 2)
	if err != nil || len(ownReads) != 0 {
		t.Fatalf("reads leaked: %v %v", ownReads, err)
	}
	if _, err := channels.SetAccess(ctx, 1, 2, 1, channel.Access{Restricted: true, UserIDs: []int64{2}}); err != nil {
		t.Fatal(err)
	}
	if err := channels.CheckAccess(ctx, 1, 2, 2); err != nil {
		t.Fatalf("explicit grant: %v", err)
	}
	ids, err := channels.ListMemberIDs(ctx, 1, 2, 1)
	if err != nil || len(ids) != 2 {
		t.Fatalf("roster: %v %v", ids, err)
	}
	if _, err := channels.SetAccess(ctx, 1, 2, 1, channel.Access{Restricted: true, Roles: []server.Role{server.RoleMember}}); err != nil {
		t.Fatal(err)
	}
	if err := channels.CheckAccess(ctx, 1, 2, 2); err != nil {
		t.Fatalf("role grant: %v", err)
	}
	if err := channels.CheckAccess(ctx, 1, 2, 3); !errors.Is(err, channel.ErrForbidden) {
		t.Fatalf("outsider: %v", err)
	}
}

func TestAccessUpdatesAreAtomicScopedAndRemoveDepartedGrants(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	repo := channelsqlite.NewRepository(db)
	if _, err := repo.SetAccess(ctx, 1, 1, 1, channel.Access{Restricted: true, UserIDs: []int64{2}}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SetAccess(ctx, 1, 1, 1, channel.Access{UserIDs: []int64{3}}); !errors.Is(err, channel.ErrAccessInvalid) {
		t.Fatalf("outsider grant: %v", err)
	}
	access, err := repo.GetAccess(ctx, 1, 1, 1)
	if err != nil || !access.Restricted || len(access.UserIDs) != 1 {
		t.Fatalf("update was not atomic: %+v %v", access, err)
	}
	if err := repo.CheckVoiceAccess(ctx, 1, 1, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM server_members WHERE server_id=1 AND user_id=2; INSERT INTO server_members(server_id,user_id,role) VALUES(1,2,'member')`); err != nil {
		t.Fatal(err)
	}
	if err := repo.CheckVoiceAccess(ctx, 1, 1, 2); !errors.Is(err, channel.ErrForbidden) {
		t.Fatalf("old membership grant survived: %v", err)
	}
	if _, err := repo.GetAccess(ctx, 99, 1, 1); !errors.Is(err, channel.ErrNotFound) {
		t.Fatalf("scope: %v", err)
	}
	if _, err := db.Exec(`UPDATE server_members SET role='admin' WHERE user_id=2 AND server_id=1`); err != nil {
		t.Fatal(err)
	}
	if err := repo.CheckVoiceAccess(ctx, 1, 1, 2); err != nil {
		t.Fatalf("admin bypass: %v", err)
	}
	if _, err := db.Exec(`UPDATE server_members SET role='member' WHERE user_id=2 AND server_id=1`); err != nil {
		t.Fatal(err)
	}
	if err := repo.CheckVoiceAccess(ctx, 1, 1, 2); !errors.Is(err, channel.ErrForbidden) {
		t.Fatalf("demoted admin: %v", err)
	}
}

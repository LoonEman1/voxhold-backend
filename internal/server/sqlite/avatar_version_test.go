package sqlite_test

import (
	"context"
	"testing"

	serversqlite "voxhold-backend/internal/server/sqlite"
	"voxhold-backend/internal/testdb"
)

func TestMembersExposeAvatarVersion(t *testing.T) {
	db := testdb.Open(t)
	if _, err := db.Exec(`INSERT INTO user_avatars(user_id, content, etag) VALUES (2, x'00', 'abc')`); err != nil {
		t.Fatal(err)
	}
	members, err := serversqlite.NewRepository(db).ListMembers(context.Background(), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	versions := map[int64]*string{}
	for _, member := range members {
		versions[member.UserID] = member.AvatarVersion
	}
	if len(versions) != 2 || versions[1] != nil || versions[2] == nil || *versions[2] != "abc" {
		t.Fatalf("avatar versions: %v", versions)
	}
}

package sqlite_test

import (
	"context"
	"testing"

	messagesqlite "voxhold-backend/internal/message/sqlite"
	"voxhold-backend/internal/testdb"
)

func TestSearchFiltersNarrowResults(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	if _, err := db.Exec(`INSERT INTO users(id,username,password_hash) VALUES(41,'Searcher','h');
 INSERT INTO server_members(server_id,user_id,role) VALUES(1,41,'member')`); err != nil {
		t.Fatal(err)
	}
	repo := messagesqlite.NewRepository(db)
	for _, m := range []struct {
		author  int64
		content string
	}{{1, "plain release notes"}, {41, "release link https://example.com/x"}, {41, "unrelated"}} {
		if _, err := repo.Create(ctx, 1, 2, m.author, m.content); err != nil {
			t.Fatal(err)
		}
	}
	count := func(query string) int {
		t.Helper()
		results, err := repo.Search(ctx, 1, 1, query, nil, 25)
		if err != nil {
			t.Fatalf("%q: %v", query, err)
		}
		return len(results)
	}
	if got := count("release"); got != 2 {
		t.Fatalf("plain search = %d", got)
	}
	if got := count("release from:searcher"); got != 1 {
		t.Fatalf("from filter = %d", got)
	}
	if got := count("release has:link"); got != 1 {
		t.Fatalf("has:link = %d", got)
	}
	if got := count("from:SEARCHER"); got != 2 {
		t.Fatalf("operator-only query = %d", got)
	}
	if got := count("release in:text"); got != 2 {
		t.Fatalf("in filter = %d", got)
	}
	if got := count("release in:nonexistent"); got != 0 {
		t.Fatalf("unknown channel = %d", got)
	}
	if got := count("release after:2999-01-01"); got != 0 {
		t.Fatalf("after in the future = %d", got)
	}
	if got := count("release before:2999-01-01"); got != 2 {
		t.Fatalf("before in the future = %d", got)
	}
}

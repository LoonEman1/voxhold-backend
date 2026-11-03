package sqlite_test

import (
	"context"
	"database/sql"
	"testing"

	messagesqlite "voxhold-backend/internal/message/sqlite"
	"voxhold-backend/internal/testdb"
)

// BenchmarkMessageSQLite exercises real repository queries, access checks,
// interaction hydration and the production migration chain on a file WAL DB.
// Setup is outside the measured loops. Do not run the scenarios in parallel:
// OpenFile configures a separate DATABASE_PATH for each benchmark fixture.
func BenchmarkMessageSQLite(b *testing.B) {
	for _, scenario := range []struct {
		name     string
		optimize bool
		memory   bool
	}{
		{name: "baseline"},
		{name: "optimize", optimize: true},
		{name: "memory_optimize", optimize: true, memory: true},
	} {
		b.Run(scenario.name, func(b *testing.B) {
			db := messageBenchmarkDB(b, scenario.memory)
			if scenario.optimize {
				if _, err := db.Exec("PRAGMA optimize=0x10002"); err != nil {
					b.Fatal(err)
				}
			}
			repository := messagesqlite.NewRepository(db)
			ctx := context.Background()
			b.Run("search", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					values, err := repository.Search(ctx, 1, 2, "needle", nil, 50)
					if err != nil || len(values) != 50 {
						b.Fatalf("search: rows=%d err=%v", len(values), err)
					}
				}
			})
			b.Run("history", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					values, err := repository.ListByChannelID(ctx, 1, 10, 2, nil, 50)
					if err != nil || len(values) != 50 {
						b.Fatalf("history: rows=%d err=%v", len(values), err)
					}
				}
			})
		})
	}
}

func messageBenchmarkDB(b *testing.B, memory bool) *sql.DB {
	b.Helper()
	db := testdb.OpenFile(b)
	// temp_store is connection-local: use the same connection for setup/queries.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	mode := "DEFAULT"
	if memory {
		mode = "MEMORY"
	}
	if _, err := db.Exec("PRAGMA temp_store=" + mode); err != nil {
		b.Fatal(err)
	}
	const seed = `
	WITH RECURSIVE ids(n) AS (SELECT 10 UNION ALL SELECT n+1 FROM ids WHERE n<29)
	INSERT INTO channels(id,server_id,name,kind,created_by)
	SELECT n,1,printf('Channel%d',n),'text',1 FROM ids;
	WITH RECURSIVE ids(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM ids WHERE n<100000)
	INSERT INTO messages(channel_id,author_user_id,content,created_at)
	SELECT 10+n%20,2,printf('history row %d alpha beta %s',n,CASE WHEN n%97=0 THEN 'needle' ELSE 'noise' END),1700000000+n FROM ids;
	`
	if _, err := db.Exec(seed); err != nil {
		b.Fatal(err)
	}
	return db
}

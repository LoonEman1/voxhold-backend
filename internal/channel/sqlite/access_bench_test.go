package sqlite_test

import (
	"context"
	"fmt"
	"testing"

	"voxhold-backend/internal/channel"
	channelsqlite "voxhold-backend/internal/channel/sqlite"
	"voxhold-backend/internal/testdb"
)

func BenchmarkSetAccess(b *testing.B) {
	for _, n := range []int{10, 100, 1000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			db := testdb.OpenFile(b)
			tx, err := db.Begin()
			if err != nil {
				b.Fatal(err)
			}
			ids := make([]int64, n)
			for i := range ids {
				id := int64(i + 10)
				ids[i] = id
				if _, err = tx.Exec(`INSERT INTO users(id,username,password_hash) VALUES(?,?, 'hash')`, id, fmt.Sprintf("user%d", id)); err != nil {
					b.Fatal(err)
				}
				if _, err = tx.Exec(`INSERT INTO server_members(server_id,user_id,role) VALUES(1,?,'member')`, id); err != nil {
					b.Fatal(err)
				}
			}
			if err = tx.Commit(); err != nil {
				b.Fatal(err)
			}
			r := channelsqlite.NewRepository(db)
			b.ResetTimer()
			for range b.N {
				if _, err := r.SetAccess(context.Background(), 1, 2, 1, channel.Access{Restricted: true, UserIDs: ids}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

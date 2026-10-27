package sqlite_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"voxhold-backend/internal/channel"
	channelsqlite "voxhold-backend/internal/channel/sqlite"
	"voxhold-backend/internal/testdb"
)

func TestBatchAccessMatchesIndividualPolicies(t *testing.T) {
	db := testdb.OpenFile(t)
	r := channelsqlite.NewRepository(db)
	if _, err := r.SetAccess(context.Background(), 1, 2, 1, channel.Access{Restricted: true, UserIDs: []int64{2}}); err != nil {
		t.Fatal(err)
	}
	ids := []int64{1, 2, 2, 3, 999}
	batch, err := r.AccessibleChannelIDsForUsers(ids)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		single, err := r.AccessibleChannelIDs(id)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(single, batch[id]) {
			t.Fatalf("different policy for user %d", id)
		}
	}
}

func TestSetAccessInvalidGrantRollsBackEverything(t *testing.T) {
	db := testdb.OpenFile(t)
	r := channelsqlite.NewRepository(db)
	ctx := context.Background()
	original := channel.Access{Restricted: true, UserIDs: []int64{2}}
	if _, err := r.SetAccess(ctx, 1, 2, 1, original); err != nil {
		t.Fatal(err)
	}
	if _, err := r.SetAccess(ctx, 1, 2, 1, channel.Access{Restricted: false, UserIDs: []int64{2, 3}}); err != channel.ErrAccessInvalid {
		t.Fatal("outsider accepted:", err)
	}
	got, err := r.GetAccess(ctx, 1, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Restricted || !reflect.DeepEqual(got.UserIDs, original.UserIDs) {
		t.Fatal("invalid batch partially committed")
	}
}

func BenchmarkLoadAccess(b *testing.B) {
	for _, n := range []int{10, 100, 1000} {
		for _, batch := range []bool{false, true} {
			b.Run(fmt.Sprintf("%d/batch=%v", n, batch), func(b *testing.B) {
				db := testdb.OpenFile(b)
				r := channelsqlite.NewRepository(db)
				tx, err := db.Begin()
				if err != nil {
					b.Fatal(err)
				}
				ids := make([]int64, n)
				for i := range ids {
					id := int64(i + 10)
					ids[i] = id
					if _, err = tx.Exec(`INSERT INTO users(id,username,password_hash) VALUES(?,?,'hash')`, id, fmt.Sprintf("user%d", id)); err != nil {
						b.Fatal(err)
					}
					if _, err = tx.Exec(`INSERT INTO server_members(server_id,user_id,role) VALUES(1,?,'member')`, id); err != nil {
						b.Fatal(err)
					}
				}
				if err = tx.Commit(); err != nil {
					b.Fatal(err)
				}
				b.ResetTimer()
				for range b.N {
					if batch {
						if _, err := r.AccessibleChannelIDsForUsers(ids); err != nil {
							b.Fatal(err)
						}
					} else {
						for _, id := range ids {
							if _, err := r.AccessibleChannelIDs(id); err != nil {
								b.Fatal(err)
							}
						}
					}
				}
			})
		}
	}
}

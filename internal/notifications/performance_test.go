package notifications

import (
	"fmt"
	"testing"

	"voxhold-backend/internal/message"
	"voxhold-backend/internal/testdb"
)

// Uses the real WAL schema and ACL/notification views, with sparse recipients.
func BenchmarkNotificationDelivery(b *testing.B) {
	for _, members := range []int{100, 1000} {
		for _, mention := range []bool{false, true} {
			b.Run(fmt.Sprintf("members=%d/mention=%t", members, mention), func(b *testing.B) {
				db := testdb.OpenFile(b)
				tx, err := db.Begin()
				if err != nil {
					b.Fatal(err)
				}
				for i := 0; i < members; i++ {
					id := i + 100
					if _, err = tx.Exec(`INSERT INTO users(id,username,password_hash) VALUES(?,?, 'fixture'); INSERT INTO server_members(server_id,user_id,role) VALUES(1,?,'member')`, id, fmt.Sprintf("bench-%d", id), id); err != nil {
						b.Fatal(err)
					}
				}
				if err = tx.Commit(); err != nil {
					b.Fatal(err)
				}
				service := NewService(db, nil, nil, nil)
				content := "ordinary message without recipients"
				if mention {
					content = "hello <@100>"
				}
				value := message.Message{ID: 999999, ChannelID: 2, Author: message.Author{UserID: 1, Username: "owner"}, Content: content}
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if err := service.Deliver(value); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

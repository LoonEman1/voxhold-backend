package realtime

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

var encodedBytes int

func BenchmarkBroadcastEncoding(b *testing.B) {
	for _, count := range []int{100, 1000} {
		b.Run(fmt.Sprintf("clients=%d", count), func(b *testing.B) {
			h := NewHub()
			b.Cleanup(h.Close)
			clients := make([]*Client, count)
			for i := range clients {
				clients[i] = NewClient(int64(i+1), "fixture", []int64{1})
			}
			payload := MessageCreatedData{ID: 100, ChannelID: 2, Author: MessageAuthorData{UserID: 1, Username: "author"}, Content: strings.Repeat("chat payload ", 80)}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				h.broadcastToClients(clients, OutgoingEvent{Type: EventMessageCreated, Data: payload})
				for _, client := range clients {
					event, ok := client.PopOutgoing()
					if !ok {
						b.Fatal("missing broadcast")
					}
					// Exercise the same shared-or-individual encoding choice as the
					// WebSocket writer. The fallback also runs on the pre-change code.
					var data []byte
					var err error
					shared := false
					if value, ok := any(event).(interface{ SharedJSON() ([]byte, bool, error) }); ok {
						data, shared, err = value.SharedJSON()
					}
					if !shared {
						data, err = json.Marshal(event)
					}
					if err != nil {
						b.Fatal(err)
					}
					encodedBytes = len(data)
				}
			}
		})
	}
}

package notifications

import (
	"context"
	"testing"

	"voxhold-backend/internal/message"
	"voxhold-backend/internal/testdb"
)

func TestRepliesNotifyTheirRecipientAndForwardsDoNotRepingQuotedMentions(t *testing.T) {
	db := testdb.Open(t)
	if _, err := db.Exec(`INSERT INTO messages(id,channel_id,author_user_id,content) VALUES(1,2,2,'answer'),(2,2,1,'quoted <@2> <@&member>')`); err != nil {
		t.Fatal(err)
	}
	var received []Event
	service := NewService(db, func(id int64) bool { return id == 1 || id == 2 }, func(_ int64, event Event) { received = append(received, event) }, nil)
	value := message.Message{ID: 1, ChannelID: 2, Author: message.Author{UserID: 2, Username: "member"}, Content: "answer", Reply: &message.ReplyReference{MessageID: 10, Author: message.Author{UserID: 1, Username: "owner"}}}
	if err := service.Deliver(value); err != nil {
		t.Fatal(err)
	}
	if len(received) != 1 || !received[0].Mentioned {
		t.Fatal("reply recipient was not notified")
	}
	received = nil
	value = message.Message{ID: 2, ChannelID: 2, Author: message.Author{UserID: 1, Username: "owner"}, Content: "quoted <@2> <@&member>", Forward: &message.ForwardReference{Author: message.Author{UserID: 2, Username: "member"}}}
	if err := service.Deliver(value); err != nil {
		t.Fatal(err)
	}
	if len(received) != 0 {
		t.Fatal("forward repinged original mentions")
	}
	if _, err := service.Set(context.Background(), 1, 2, 2, All); err != nil {
		t.Fatal(err)
	}
	if err := service.Deliver(value); err != nil {
		t.Fatal(err)
	}
	if len(received) != 1 || received[0].Mentioned {
		t.Fatal("all-message preference did not receive a neutral forward alert")
	}
}

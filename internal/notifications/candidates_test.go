package notifications

import (
	"context"
	"slices"
	"testing"

	"voxhold-backend/internal/message"
	"voxhold-backend/internal/testdb"
)

func TestCandidateSelectionKeepsNewsThreadAndPermissionRules(t *testing.T) {
	db := testdb.Open(t)
	if _, err := db.Exec(`UPDATE channels SET is_news=1 WHERE id=2;
 INSERT INTO messages(id,channel_id,author_user_id,content) VALUES(50,2,1,'root')`); err != nil {
		t.Fatal(err)
	}
	var received []int64
	service := NewService(db, func(id int64) bool { return id >= 1 && id <= 3 }, func(id int64, _ Event) { received = append(received, id) }, nil)
	value := message.Message{ID: 51, ChannelID: 2, Author: message.Author{UserID: 1, Username: "owner"}, Content: "ordinary"}
	check := func(want ...int64) {
		t.Helper()
		received = nil
		result, err := db.Exec(`INSERT INTO messages(channel_id,author_user_id,content) VALUES(2,1,?)`, value.Content)
		if err != nil {
			t.Fatal(err)
		}
		value.ID, err = result.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		if err := service.Deliver(value); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(received, want) {
			t.Fatalf("received %v, want %v", received, want)
		}
	}
	check(2) // News defaults to all messages.
	if _, err := service.Set(context.Background(), 1, 2, 2, Mentions); err != nil {
		t.Fatal(err)
	}
	check()
	value.Content = "<@2> <@3>"
	check(2) // An explicit ID does not grant membership.
	if _, err := service.Set(context.Background(), 1, 2, 2, All); err != nil {
		t.Fatal(err)
	}
	root := int64(50)
	value.ThreadRootID = &root
	value.Content = "reply in thread"
	check() // All-message preferences do not subscribe to every discussion.
	if _, err := db.Exec(`INSERT INTO thread_subscriptions(message_id,user_id,following) VALUES(50,2,1)`); err != nil {
		t.Fatal(err)
	}
	check(2)
	if _, err := service.Set(context.Background(), 1, 2, 2, None); err != nil {
		t.Fatal(err)
	}
	check() // Explicit opt-out also excludes followers.
	if _, err := service.Set(context.Background(), 1, 2, 2, All); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO server_roles(id,server_id,name,permissions) VALUES(100,1,'cannot read',0);
 INSERT INTO member_roles(server_id,user_id,role_id) VALUES(1,2,100)`); err != nil {
		t.Fatal(err)
	}
	value.Content = "<@2>"
	check() // Candidate selection never bypasses custom ACLs.
}

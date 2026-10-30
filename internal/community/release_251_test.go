package community

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"
	serversqlite "voxhold-backend/internal/server/sqlite"
)

func TestBanCancelsScheduledPersonalMessagesAndRejectsWorkerDelivery(t *testing.T) {
	f := setup(t)
	f.call(t, 1, "PUT", "/api/v1/me/message-privacy", map[string]string{"mode": "everyone"}, 200)
	cid := number(t, f.call(t, 2, "POST", "/api/v1/me/conversations", map[string]any{"user_ids": []int64{1}}, 200), "id")
	now := time.Now().Unix()
	scheduled := number(t, f.call(t, 2, "POST", "/api/v1/me/scheduled-messages", map[string]any{"destination": map[string]any{"kind": "conversation", "id": cid}, "payload": map[string]any{"content": "later", "client_message_id": "ban-check"}, "send_at": now + 60}, 200), "id")
	if err := serversqlite.NewRepository(f.db).BanMember(context.Background(), 1, 1, 2); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := f.db.QueryRow(`SELECT status FROM scheduled_messages WHERE id=?`, scheduled).Scan(&status); err != nil || status != "cancelled" {
		t.Fatalf("status=%s err=%v", status, err)
	}
	// Even a pending job recovered after a crash must recheck the author, rather
	// than relying on the best-effort cancellation performed by the ban.
	if _, err := f.db.Exec(`UPDATE scheduled_messages SET status='pending' WHERE id=?`, scheduled); err != nil {
		t.Fatal(err)
	}
	if err := f.s.ProcessDue(context.Background(), now+61); err != nil {
		t.Fatal(err)
	}
	var sent int
	if err := f.db.QueryRow(`SELECT count(*) FROM conversation_messages WHERE author_user_id=2`).Scan(&sent); err != nil || sent != 0 {
		t.Fatalf("banned sends=%d err=%v", sent, err)
	}
	if err := CanSend(context.Background(), f.db, cid, 2); err != ErrForbidden {
		t.Fatalf("banned CanSend=%v", err)
	}
}
func TestConversationPaginationAndOpeningAnOlderConversation(t *testing.T) {
	f := setup(t)
	if _, err := f.db.Exec(`INSERT INTO server_members(server_id,user_id,role) VALUES(1,3,'member')`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 205; i++ {
		f.call(t, 1, "POST", "/api/v1/me/conversations", map[string]any{"name": fmt.Sprintf("group-%d", i), "user_ids": []int64{2, 3}}, 200)
	}
	first, err := f.s.conversations(httptest.NewRequest("GET", "/api/v1/me/conversations", nil), 1)
	if err != nil {
		t.Fatal(err)
	}
	page := first.([]Conversation)
	if len(page) != 200 {
		t.Fatal(len(page))
	}
	last := page[len(page)-1]
	next, err := f.s.conversations(httptest.NewRequest("GET", fmt.Sprintf("/api/v1/me/conversations?before_activity=%d&before_id=%d", last.LastMessageID, last.ID), nil), 1)
	if err != nil {
		t.Fatal(err)
	}
	rest := next.([]Conversation)
	if len(rest) != 5 {
		t.Fatal(len(rest))
	}
	for _, v := range rest {
		if len(v.Members) != 3 {
			t.Fatalf("members=%v", v.Members)
		}
	}
	f.call(t, 1, "GET", "/api/v1/conversations/1", nil, 200)
	f.call(t, 1, "GET", "/api/v1/conversations/invalid", nil, 400)
	f.call(t, 1, "GET", "/api/v1/me/conversations?before_id=2&before_activity=-1", nil, 400)
	if _, err := f.db.Exec(`DELETE FROM conversation_members WHERE conversation_id=1 AND user_id=3`); err != nil {
		t.Fatal(err)
	}
	f.call(t, 3, "GET", "/api/v1/conversations/1", nil, 403)
}

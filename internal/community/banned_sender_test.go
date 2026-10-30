package community

import (
	"context"
	"errors"
	"testing"
	"time"
	serversqlite "voxhold-backend/internal/server/sqlite"
)

func TestBanCancelsSchedulesAndDirectSendChecksActor(t *testing.T) {
	f := setup(t)
	now := time.Now().Unix()
	f.call(t, 1, "PUT", "/api/v1/me/message-privacy", map[string]string{"mode": "everyone"}, 200)
	cid := number(t, f.call(t, 2, "POST", "/api/v1/me/conversations", map[string]any{"user_ids": []int64{1}}, 200), "id")
	f.call(t, 2, "POST", "/api/v1/me/scheduled-messages", map[string]any{"destination": map[string]any{"kind": "conversation", "id": cid}, "payload": map[string]any{"content": "after ban"}, "send_at": now + 60}, 200)
	if err := serversqlite.NewRepository(f.db).BanMember(context.Background(), 1, 1, 2); err != nil {
		t.Fatal(err)
	}
	if err := f.s.ProcessDue(context.Background(), now+61); err != nil {
		t.Fatal(err)
	}
	var n int
	var status string
	if err := f.db.QueryRow(`SELECT count(*) FROM conversation_messages WHERE author_user_id=2`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(`SELECT status FROM scheduled_messages WHERE user_id=2`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if n != 0 || status != "cancelled" {
		t.Fatalf("sent=%d schedule=%s", n, status)
	}
	// An already-authorized in-flight request must fail after the ban as well.
	if _, err := f.s.createDirect(context.Background(), cid, 2, DirectInput{Content: "in flight", ClientMessageID: "after-ban"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("send=%v", err)
	}
	if err := CanSend(context.Background(), f.db, cid, 2); !errors.Is(err, ErrForbidden) {
		t.Fatalf("contact=%v", err)
	}
}

package community

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestScheduleRejectsNULAndQuarantinesLegacyPoison(t *testing.T) {
	f := setup(t)
	attachMessages(f)
	now := time.Now().Unix()
	input := map[string]any{"destination": map[string]any{"kind": "channel", "id": 2}, "payload": map[string]any{"content": "\x00poison"}, "send_at": now + 60}
	f.call(t, 2, "POST", "/api/v1/me/scheduled-messages", input, 400)
	// Older versions accepted this payload; upgrading must recover those queues too.
	data, _ := json.Marshal(DirectInput{Content: "\x00poison"})
	if _, err := f.db.Exec(`INSERT INTO scheduled_messages(user_id,destination_kind,destination_id,payload,send_at) VALUES(2,'channel',2,?,?)`, string(data), now+60); err != nil {
		t.Fatal(err)
	}
	input["payload"] = map[string]any{"content": "healthy"}
	input["send_at"] = now + 61
	f.call(t, 1, "POST", "/api/v1/me/scheduled-messages", input, 200)
	if err := f.s.ProcessDue(context.Background(), now+62); err != nil {
		t.Fatal(err)
	}
	var count int
	var status string
	if err := f.db.QueryRow(`SELECT count(*) FROM messages WHERE content='healthy'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(`SELECT status FROM scheduled_messages WHERE user_id=2`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if count != 1 || status != "failed" {
		t.Fatalf("healthy=%d poison=%s", count, status)
	}
}

func TestScheduleRetriesAreBoundedAndRemindersRemainIndependent(t *testing.T) {
	f := setup(t)
	now := time.Now().Unix()
	f.call(t, 1, "POST", "/api/v1/me/scheduled-messages", map[string]any{"destination": map[string]any{"kind": "channel", "id": 2}, "payload": map[string]any{"content": "retry"}, "send_at": now + 60}, 200)
	if _, err := f.db.Exec(`INSERT INTO server_events(id,server_id,creator_user_id,title,starts_at) VALUES(1,1,1,'Reminder',?); INSERT INTO server_event_responses(event_id,user_id,response) VALUES(1,2,'going')`, now+600); err != nil {
		t.Fatal(err)
	}
	// This fixture has no message service, a transient dispatch failure.
	if err := f.s.ProcessDue(context.Background(), now+61); err != nil {
		t.Fatal(err)
	}
	var attempts, reminded int
	var status string
	if err := f.db.QueryRow(`SELECT reminded FROM server_event_responses WHERE event_id=1`).Scan(&reminded); err != nil {
		t.Fatal(err)
	}
	if err := f.s.ProcessDue(context.Background(), now+62); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(`SELECT attempts FROM scheduled_messages`).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 || reminded != 1 {
		t.Fatalf("attempts=%d reminded=%d", attempts, reminded)
	}
	for i := 1; i < 5; i++ {
		if err := f.s.ProcessDue(context.Background(), now+int64(i)*1000); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.db.QueryRow(`SELECT attempts,status FROM scheduled_messages`).Scan(&attempts, &status); err != nil {
		t.Fatal(err)
	}
	if attempts != 5 || status != "failed" {
		t.Fatalf("attempts=%d status=%s", attempts, status)
	}
}

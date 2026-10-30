package community

import (
	"context"
	"errors"
	"testing"
	"time"
	"voxhold-backend/internal/message"
)

func TestChannelPollsPersistAndReplayThroughService(t *testing.T) {
	for _, news := range []bool{false, true} {
		t.Run(map[bool]string{false: "text", true: "news"}[news], func(t *testing.T) {
			f := setup(t)
			attachMessages(f)
			if news {
				if _, err := f.db.Exec(`UPDATE channels SET is_news=1 WHERE id=2`); err != nil {
					t.Fatal(err)
				}
			}
			input := message.CreateInput{ClientMessageID: "poll", Poll: &message.PollInput{Question: "When?", Options: []string{"Today", "Tomorrow"}, ClosesAt: time.Now().Unix() + 3600}}
			a, err := f.s.messages.Create(context.Background(), 1, 2, 1, input)
			if err != nil {
				t.Fatal(err)
			}
			b, err := f.s.messages.Create(context.Background(), 1, 2, 1, input)
			if err != nil {
				t.Fatal(err)
			}
			if a.Poll == nil || len(a.Poll.Options) != 2 || b.Poll == nil || a.ID != b.ID || a.Poll.ID != b.Poll.ID {
				t.Fatalf("poll replay: %#v %#v", a, b)
			}
			input.Poll = &message.PollInput{Question: "When?", Options: []string{"Morning", "Evening"}, ClosesAt: input.Poll.ClosesAt}
			if _, err := f.s.messages.Create(context.Background(), 1, 2, 1, input); !errors.Is(err, message.ErrIdempotencyConflict) {
				t.Fatalf("changed poll retry: %v", err)
			}
		})
	}
}
func TestScheduledChannelPollPersists(t *testing.T) {
	f := setup(t)
	attachMessages(f)
	now := time.Now().Unix()
	f.call(t, 1, "POST", "/api/v1/me/scheduled-messages", map[string]any{"destination": map[string]any{"kind": "channel", "id": 2}, "payload": map[string]any{"poll": map[string]any{"question": "When?", "options": []string{"Today", "Tomorrow"}, "closes_at": now + 3600}}, "send_at": now + 60}, 200)
	if err := f.s.ProcessDue(context.Background(), now+61); err != nil {
		t.Fatal(err)
	}
	var count int
	var status string
	if err := f.db.QueryRow(`SELECT count(*) FROM polls WHERE channel_message_id IS NOT NULL`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(`SELECT status FROM scheduled_messages`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if count != 1 || status != "sent" {
		t.Fatalf("polls=%d status=%s", count, status)
	}
}

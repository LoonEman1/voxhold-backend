package community

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestCreateConversationReturnsCompleteConversation(t *testing.T) {
	f := setup(t)
	for range 2 { // Both a new direct chat and an existing one use the same contract.
		created := f.call(t, 1, "POST", "/api/v1/me/conversations", map[string]any{"user_ids": []int64{2}}, 200)
		var chat Conversation
		data, err := json.Marshal(created)
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(data, &chat); err != nil {
			t.Fatal(err)
		}
		if chat.ID == 0 || !chat.Direct || len(chat.Members) != 2 {
			t.Fatalf("incomplete conversation: %s", data)
		}
		fetched := f.call(t, 1, "GET", fmt.Sprintf("/api/v1/conversations/%d", chat.ID), nil, 200)
		for _, field := range []string{"id", "name", "direct", "members", "unread_count", "last_message_id"} {
			if string(created[field]) != string(fetched[field]) {
				t.Fatalf("%s differs: POST=%s GET=%s", field, created[field], fetched[field])
			}
		}
	}
}

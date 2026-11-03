package community

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestSearchAcrossOwnConversationsOnly(t *testing.T) {
	f := setup(t)
	f.call(t, 1, "PUT", "/api/v1/me/message-privacy", map[string]string{"mode": "everyone"}, 200)
	f.call(t, 2, "PUT", "/api/v1/me/message-privacy", map[string]string{"mode": "everyone"}, 200)
	cid := number(t, f.call(t, 2, "POST", "/api/v1/me/conversations", map[string]any{"user_ids": []int64{1}}, 200), "id")
	f.call(t, 2, "POST", fmt.Sprintf("/api/v1/conversations/%d/messages", cid), map[string]any{"content": "the quarterly budget", "client_message_id": "dm-search-1"}, 200)
	hits := f.call(t, 1, "GET", "/api/v1/me/conversations/search?q=budget", nil, 200)
	var results []DirectSearchResult
	if err := json.Unmarshal(hits["results"], &results); err != nil || len(results) != 1 {
		t.Fatalf("participant results = %v err=%v", results, err)
	}
	f.call(t, 1, "GET", "/api/v1/me/conversations/search?q=", nil, 400)
}

package community

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestConversationPagesCoverOlderGroupsAndAllowDirectLookup(t *testing.T) {
	f := setup(t)
	if _, err := f.db.Exec(`INSERT INTO server_members(server_id,user_id,role) VALUES(1,3,'member')`); err != nil {
		t.Fatal(err)
	}
	var oldest int64
	for i := 0; i < 201; i++ {
		v := f.call(t, 1, "POST", "/api/v1/me/conversations", map[string]any{"name": fmt.Sprintf("group-%03d", i), "user_ids": []int64{2, 3}}, 200)
		if i == 0 {
			oldest = number(t, v, "id")
		}
	}
	seen := map[int64]bool{}
	path := "/me/conversations?limit=100"
	for {
		page := f.call(t, 1, "GET", "/api/v1"+path, nil, 200)
		var values []Conversation
		var more bool
		var next string
		if err := json.Unmarshal(page["conversations"], &values); err != nil {
			t.Fatal(err)
		}
		json.Unmarshal(page["has_more"], &more)
		for _, v := range values {
			if seen[v.ID] || len(v.Members) != 3 {
				t.Fatalf("duplicate or missing members: %#v", v)
			}
			seen[v.ID] = true
		}
		if !more {
			break
		}
		if err := json.Unmarshal(page["next_cursor"], &next); err != nil {
			t.Fatal(err)
		}
		path = "/me/conversations?limit=100&before=" + next
	}
	if len(seen) != 201 {
		t.Fatalf("covered %d conversations", len(seen))
	}
	f.call(t, 1, "GET", fmt.Sprintf("/api/v1/conversations/%d", oldest), nil, 200)
	f.call(t, 99, "GET", fmt.Sprintf("/api/v1/conversations/%d", oldest), nil, 403)
	f.call(t, 1, "GET", "/api/v1/me/conversations?before=invalid", nil, 400)
	f.call(t, 1, "GET", "/api/v1/me/conversations?limit=0", nil, 400)
}
func TestScheduledPagesCoverMoreThan200AndFilterStatus(t *testing.T) {
	f := setup(t)
	now := time.Now().Unix()
	for i := 0; i < 205; i++ {
		if _, err := f.db.Exec(`INSERT INTO scheduled_messages(user_id,destination_kind,destination_id,payload,send_at,status) VALUES(1,'channel',2,'{"content":"later"}',?,?)`, now+int64(i), map[bool]string{true: "sent", false: "pending"}[i < 2]); err != nil {
			t.Fatal(err)
		}
	}
	path := "/api/v1/me/scheduled-messages?status=pending&limit=100"
	seen := map[int64]bool{}
	for {
		page := f.call(t, 1, "GET", path, nil, 200)
		var values []Scheduled
		var more bool
		var next string
		if err := json.Unmarshal(page["scheduled_messages"], &values); err != nil {
			t.Fatal(err)
		}
		json.Unmarshal(page["has_more"], &more)
		for _, v := range values {
			if seen[v.ID] || v.Status != "pending" {
				t.Fatal(v)
			}
			seen[v.ID] = true
		}
		if !more {
			break
		}
		json.Unmarshal(page["next_cursor"], &next)
		path = "/api/v1/me/scheduled-messages?status=pending&limit=100&before=" + next
	}
	if len(seen) != 203 {
		t.Fatalf("covered %d pending jobs", len(seen))
	}
	page := f.call(t, 2, "GET", "/api/v1/me/scheduled-messages?limit=100", nil, 200)
	if string(page["scheduled_messages"]) != "[]" {
		t.Fatal("another user's jobs leaked")
	}
}

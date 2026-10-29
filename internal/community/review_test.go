package community

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestScheduleRetryAndGroupDelivery(t *testing.T) {
	f := setup(t)
	if _, err := f.db.Exec(`INSERT INTO server_members(server_id,user_id,role) VALUES(1,3,'member')`); err != nil {
		t.Fatal(err)
	}
	cid := number(t, f.call(t, 1, "POST", "/api/v1/me/conversations", map[string]any{"name": "Group", "user_ids": []int64{2, 3}}, 200), "id")
	now := time.Now().Unix()
	input := map[string]any{"destination": map[string]any{"kind": "conversation", "id": cid}, "payload": map[string]any{"content": "Group later", "client_message_id": "stable-schedule"}, "send_at": now + 60}
	one := number(t, f.call(t, 1, "POST", "/api/v1/me/scheduled-messages", input, 200), "id")
	two := number(t, f.call(t, 1, "POST", "/api/v1/me/scheduled-messages", input, 200), "id")
	if one != two {
		t.Fatal("retry created another scheduled message")
	}
	input["send_at"] = now + 90
	f.call(t, 1, "POST", "/api/v1/me/scheduled-messages", input, 409)
	input["send_at"] = now + 60
	if err := f.s.ProcessDue(context.Background(), now+61); err != nil {
		t.Fatal(err)
	}
	// Simulate a crash after sending but before recording the dispatch result.
	if _, err := f.db.Exec(`UPDATE scheduled_messages SET status='pending' WHERE id=?`, one); err != nil {
		t.Fatal(err)
	}
	if err := f.s.ProcessDue(context.Background(), now+70); err != nil {
		t.Fatal(err)
	}
	for _, uid := range []int64{1, 2, 3} {
		v := f.call(t, uid, "GET", fmt.Sprintf("/api/v1/conversations/%d/messages", cid), nil, 200)
		var messages []DirectMessage
		json.Unmarshal(v["messages"], &messages)
		if len(messages) != 1 {
			t.Fatal("duplicate/missing group message", uid, len(messages))
		}
		if uid == 3 {
			f.call(t, uid, "PUT", fmt.Sprintf("/api/v1/conversations/%d/messages/%d/reactions/🔥", cid, messages[0].ID), nil, 200)
		}
	}
	f.call(t, 1, "POST", "/api/v1/me/scheduled-messages", input, 200)
}
func TestFavoritesSearchHandlesCyrillicAndNotes(t *testing.T) {
	f := setup(t)
	cid := number(t, f.call(t, 1, "GET", "/api/v1/me/favorites", nil, 200), "id")
	v := f.call(t, 1, "POST", fmt.Sprintf("/api/v1/conversations/%d/messages", cid), map[string]any{"content": "ПРОЕКТ ВОКСХОЛД", "client_message_id": "saved-search"}, 200)
	mid := number(t, v, "id")
	f.call(t, 1, "PATCH", fmt.Sprintf("/api/v1/conversations/%d/messages/%d/note", cid, mid), map[string]any{"note": "ПРОВЕРИТЬ ЗАВТРА"}, 200)
	for _, query := range []string{"воксхолд", "завтра"} {
		page := f.call(t, 1, "GET", fmt.Sprintf("/api/v1/conversations/%d/messages?q=%s", cid, query), nil, 200)
		var messages []DirectMessage
		json.Unmarshal(page["messages"], &messages)
		if len(messages) != 1 || messages[0].ID != mid {
			t.Fatal("Cyrillic search failed", query, string(page["messages"]))
		}
	}
	page := f.call(t, 1, "GET", fmt.Sprintf("/api/v1/conversations/%d/messages?q=несуществующее", cid), nil, 200)
	var messages []DirectMessage
	json.Unmarshal(page["messages"], &messages)
	if len(messages) != 0 {
		t.Fatal(messages)
	}
}

func TestCallReplacementAndScreenStateAreScopedToCurrentSession(t *testing.T) {
	f := setup(t)
	cid := number(t, f.call(t, 1, "POST", "/api/v1/me/conversations", map[string]any{"user_ids": []int64{2}}, 200), "id")
	url := fmt.Sprintf("/api/v1/conversations/%d/call", cid)
	one, two, next := "owner-session-original", "member-session-original", "owner-session-reloaded"
	f.call(t, 1, "PUT", url, map[string]any{"session_id": one}, 200)
	f.call(t, 2, "PUT", url, map[string]any{"session_id": two}, 200)
	f.call(t, 1, "PUT", url, map[string]any{"session_id": next, "replace": true}, 200)
	f.call(t, 1, "PUT", url, map[string]any{"session_id": one}, 409)
	f.call(t, 1, "DELETE", url, map[string]any{"session_id": one}, 200)
	var session string
	f.db.QueryRow(`SELECT session_id FROM conversation_call_members WHERE conversation_id=? AND user_id=1`, cid).Scan(&session)
	if session != next {
		t.Fatal("old tab removed replacement call")
	}
	data := map[string]any{"session_id": next, "target_user_id": 2, "target_session_id": two, "media": map[string]any{"screen": false, "revision": 2}}
	events := 0
	f.s.notify = func(uid int64, event string, value any) {
		if event == "conversation.call-signal" {
			if uid != 2 {
				t.Fatal(uid)
			}
			events++
		}
	}
	f.call(t, 1, "POST", url+"/signal", data, 200)
	if events != 1 {
		t.Fatal(events)
	}
	data["session_id"] = one
	f.call(t, 1, "POST", url+"/signal", data, 403)
	data["session_id"] = next
	data["description"] = map[string]any{"type": "offer", "sdp": "invalid combination"}
	f.call(t, 1, "POST", url+"/signal", data, 400)
}

func TestCallStateRevisionOrdersMembershipSnapshots(t *testing.T) {
	f := setup(t)
	cid := number(t, f.call(t, 1, "POST", "/api/v1/me/conversations", map[string]any{"user_ids": []int64{2}}, 200), "id")
	url := fmt.Sprintf("/api/v1/conversations/%d/call", cid)
	initial := number(t, f.call(t, 1, "GET", url, nil, 200), "revision")
	joined := number(t, f.call(t, 1, "PUT", url, map[string]any{"session_id": "owner-session-original"}, 200), "revision")
	if joined <= initial {
		t.Fatal("join did not advance revision")
	}
	heartbeat := number(t, f.call(t, 1, "PUT", url, map[string]any{"session_id": "owner-session-original"}, 200), "revision")
	if heartbeat != joined {
		t.Fatal("heartbeat changed membership revision")
	}
	other := number(t, f.call(t, 2, "PUT", url, map[string]any{"session_id": "member-session-original"}, 200), "revision")
	replaced := number(t, f.call(t, 1, "PUT", url, map[string]any{"session_id": "owner-session-replaced", "replace": true}, 200), "revision")
	left := number(t, f.call(t, 2, "DELETE", url, map[string]any{"session_id": "member-session-original"}, 200), "revision")
	if other <= joined || replaced <= other || left <= replaced {
		t.Fatal("membership snapshots have unordered revisions", joined, other, replaced, left)
	}
}

func TestDeletedAccountRemovesFavoritesAndScheduledWork(t *testing.T) {
	f := setup(t)
	saved := number(t, f.call(t, 2, "GET", "/api/v1/me/favorites", nil, 200), "id")
	f.call(t, 2, "POST", fmt.Sprintf("/api/v1/conversations/%d/messages", saved), map[string]any{"content": "private note", "client_message_id": "private-note"}, 200)
	f.call(t, 2, "POST", "/api/v1/me/scheduled-messages", map[string]any{"destination": map[string]any{"kind": "conversation", "id": saved}, "payload": map[string]any{"content": "private scheduled"}, "send_at": time.Now().Unix() + 60}, 200)
	cid := number(t, f.call(t, 1, "POST", "/api/v1/me/conversations", map[string]any{"user_ids": []int64{2}}, 200), "id")
	f.call(t, 2, "PUT", fmt.Sprintf("/api/v1/conversations/%d/call", cid), map[string]any{"session_id": "member-session-original"}, 200)
	if _, err := f.db.Exec(`UPDATE users SET deleted_at=unixepoch(),password_hash='' WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{`SELECT count(*) FROM conversations WHERE saved_owner_id=2`, `SELECT count(*) FROM conversation_messages WHERE conversation_id=?`, `SELECT count(*) FROM scheduled_messages WHERE user_id=2`, `SELECT count(*) FROM conversation_call_members WHERE user_id=2`} {
		var n int
		var args []any
		if query == `SELECT count(*) FROM conversation_messages WHERE conversation_id=?` {
			args = []any{saved}
		}
		if err := f.db.QueryRow(query, args...).Scan(&n); err != nil || n != 0 {
			t.Fatalf("private feature data survived account deletion: %s: %d %v", query, n, err)
		}
	}
}

func TestReschedulingPreservesCreationKeyAndRejectsKeyTakeover(t *testing.T) {
	f := setup(t)
	saved := number(t, f.call(t, 1, "GET", "/api/v1/me/favorites", nil, 200), "id")
	input := map[string]any{"destination": map[string]any{"kind": "conversation", "id": saved}, "payload": map[string]any{"content": "first", "client_message_id": "job-one"}, "send_at": time.Now().Unix() + 60}
	id := number(t, f.call(t, 1, "POST", "/api/v1/me/scheduled-messages", input, 200), "id")
	input["payload"] = map[string]any{"content": "second", "client_message_id": "job-two"}
	f.call(t, 1, "POST", "/api/v1/me/scheduled-messages", input, 200)
	url := fmt.Sprintf("/api/v1/me/scheduled-messages/%d", id)
	f.call(t, 1, "PUT", url, input, 409)
	input["payload"] = map[string]any{"content": "edited"}
	edited := f.call(t, 1, "PUT", url, input, 200)
	var value DirectInput
	if err := json.Unmarshal(edited["payload"], &value); err != nil {
		t.Fatal(err)
	}
	if value.ClientMessageID != "job-one" || value.Content != "edited" {
		t.Fatal(value)
	}
	input["payload"] = map[string]any{"content": "invalid", "client_message_id": string(make([]byte, 65))}
	f.call(t, 1, "POST", "/api/v1/me/scheduled-messages", input, 400)
}
func TestCancelledEventCannotBeEdited(t *testing.T) {
	f := setup(t)
	payload := map[string]any{"title": "Game", "description": "", "starts_at": time.Now().Unix() + 3600, "timezone": "Europe/Moscow"}
	id := number(t, f.call(t, 1, "POST", "/api/v1/servers/1/events", payload, 200), "id")
	url := fmt.Sprintf("/api/v1/servers/1/events/%d", id)
	f.call(t, 1, "DELETE", url, nil, 200)
	payload["title"] = "Changed"
	f.call(t, 1, "PUT", url, payload, 409)
}

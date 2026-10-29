package community

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"
	"voxhold-backend/internal/channel"
	channelsqlite "voxhold-backend/internal/channel/sqlite"
	"voxhold-backend/internal/message"
	messagesqlite "voxhold-backend/internal/message/sqlite"
)

type quietMessages struct{}

func (quietMessages) PublishMessageCreated(message.Message)      {}
func (quietMessages) PublishMessageUpdated(message.Message)      {}
func (quietMessages) PublishMessageDeleted(message.Message)      {}
func (quietMessages) PublishMessagePinned(message.PinnedMessage) {}
func (quietMessages) PublishMessageUnpinned(int64, int64)        {}
func attachMessages(f fixture) {
	f.s.SetMessages(message.NewService(messagesqlite.NewRepository(f.db), quietMessages{}), nil)
}
func number(t *testing.T, v map[string]json.RawMessage, key string) int64 {
	t.Helper()
	var id int64
	if err := json.Unmarshal(v[key], &id); err != nil {
		t.Fatal(err)
	}
	return id
}
func TestFavoritesPrivateNotesForwardingAndConversationReactions(t *testing.T) {
	f := setup(t)
	attachMessages(f)
	v := f.call(t, 1, "GET", "/api/v1/me/favorites", nil, 200)
	saved := number(t, v, "id")
	again := f.call(t, 1, "GET", "/api/v1/me/favorites", nil, 200)
	if number(t, again, "id") != saved {
		t.Fatal("favorites duplicated")
	}
	f.call(t, 2, "GET", fmt.Sprintf("/api/v1/conversations/%d/messages", saved), nil, 403)
	conv := f.call(t, 1, "POST", "/api/v1/me/conversations", map[string]any{"user_ids": []int64{2}}, 200)
	cid := number(t, conv, "id")
	sent := f.call(t, 1, "POST", fmt.Sprintf("/api/v1/conversations/%d/messages", cid), map[string]any{"content": "personal hello", "client_message_id": "direct-original"}, 200)
	mid := number(t, sent, "id")
	source := map[string]any{"kind": "conversation", "id": mid}
	destination := map[string]any{"kind": "conversation", "id": saved}
	payload := map[string]any{"source": source, "destination": destination, "client_message_id": "save-one"}
	forwarded := f.call(t, 1, "POST", "/api/v1/me/forward", payload, 200)
	fid := number(t, forwarded, "id")
	f.call(t, 1, "POST", "/api/v1/me/forward", payload, 200)
	var count int
	if err := f.db.QueryRow(`SELECT count(*) FROM conversation_messages WHERE conversation_id=?`, saved).Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate favorite: %d %v", count, err)
	}
	f.call(t, 2, "PATCH", fmt.Sprintf("/api/v1/conversations/%d/messages/%d/note", saved, fid), map[string]any{"note": "private"}, 403)
	f.call(t, 1, "PATCH", fmt.Sprintf("/api/v1/conversations/%d/messages/%d/note", saved, fid), map[string]any{"note": "Remember this"}, 200)
	f.call(t, 2, "PATCH", fmt.Sprintf("/api/v1/conversations/%d/messages/%d/note", cid, mid), map[string]any{"note": "not saved"}, 403)
	reactionURL := fmt.Sprintf("/api/v1/conversations/%d/messages/%d/reactions/👍", cid, mid)
	f.call(t, 2, "PUT", reactionURL, nil, 200)
	f.call(t, 2, "PUT", reactionURL, nil, 200)
	f.call(t, 3, "PUT", reactionURL, nil, 403)
	history := f.call(t, 2, "GET", fmt.Sprintf("/api/v1/conversations/%d/messages", cid), nil, 200)
	var items []DirectMessage
	json.Unmarshal(history["messages"], &items)
	if len(items) != 1 || len(items[0].Reactions) != 1 || items[0].Reactions[0].Count != 1 || !items[0].Reactions[0].Me {
		t.Fatal(string(history["messages"]))
	}
	history = f.call(t, 1, "GET", fmt.Sprintf("/api/v1/conversations/%d/messages", cid), nil, 200)
	json.Unmarshal(history["messages"], &items)
	if items[0].Reactions[0].Me {
		t.Fatal("reaction me leaked across users")
	}
	f.call(t, 2, "DELETE", reactionURL, nil, 200)
	// Both directions retain the original author, without exporting private source IDs.
	out := f.call(t, 1, "POST", "/api/v1/me/forward", map[string]any{"source": source, "destination": map[string]any{"kind": "channel", "id": 2}, "client_message_id": "direct-to-channel"}, 200)
	var channelID int64
	json.Unmarshal(out["ID"], &channelID)
	if channelID == 0 {
		t.Fatal(out)
	}
	f.call(t, 2, "POST", "/api/v1/me/forward", map[string]any{"source": map[string]any{"kind": "channel", "id": channelID}, "destination": map[string]any{"kind": "conversation", "id": cid}, "client_message_id": "channel-to-direct"}, 200)
	f.call(t, 3, "POST", "/api/v1/me/forward", payload, 403)
	f.call(t, 1, "DELETE", fmt.Sprintf("/api/v1/conversations/%d/messages/%d", saved, fid), nil, 200)
}
func TestConversationMuteKeepsUnreadAndOtherMembersNotifications(t *testing.T) {
	f := setup(t)
	cid := number(t, f.call(t, 1, "POST", "/api/v1/me/conversations", map[string]any{"user_ids": []int64{2}}, 200), "id")
	notifications := 0
	messages := 0
	f.s.notify = func(uid int64, event string, v any) {
		if uid == 2 && event == "conversation.notification" {
			notifications++
		}
		if uid == 2 && event == "conversation.message" {
			messages++
		}
	}
	f.call(t, 2, "PUT", fmt.Sprintf("/api/v1/conversations/%d/notifications", cid), map[string]any{"muted": true}, 200)
	f.call(t, 1, "POST", fmt.Sprintf("/api/v1/conversations/%d/messages", cid), map[string]any{"content": "muted", "client_message_id": "muted"}, 200)
	if notifications != 0 || messages != 1 {
		t.Fatalf("notifications=%d messages=%d", notifications, messages)
	}
	f.call(t, 2, "PUT", fmt.Sprintf("/api/v1/conversations/%d/notifications", cid), map[string]any{"muted": false}, 200)
	f.call(t, 1, "POST", fmt.Sprintf("/api/v1/conversations/%d/messages", cid), map[string]any{"content": "audible", "client_message_id": "audible"}, 200)
	if notifications != 1 || messages != 2 {
		t.Fatalf("notifications=%d messages=%d", notifications, messages)
	}
	var unread int
	if err := f.db.QueryRow(`SELECT count(*) FROM conversation_messages WHERE conversation_id=? AND id>(SELECT last_read_id FROM conversation_members WHERE conversation_id=? AND user_id=2)`, cid, cid).Scan(&unread); err != nil || unread != 2 {
		t.Fatal(unread, err)
	}
}
func TestSchedulesPersistentDispatchCancelAndRevokedPermissions(t *testing.T) {
	f := setup(t)
	attachMessages(f)
	now := time.Now().Unix()
	input := map[string]any{"destination": map[string]any{"kind": "channel", "id": 2}, "payload": map[string]any{"content": "later"}, "send_at": now + 60}
	a := f.call(t, 2, "POST", "/api/v1/me/scheduled-messages", input, 200)
	id := number(t, a, "id")
	f.call(t, 1, "DELETE", fmt.Sprintf("/api/v1/me/scheduled-messages/%d", id), nil, 404)
	if err := f.s.ProcessDue(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	var count int
	f.db.QueryRow(`SELECT count(*) FROM messages WHERE content='later'`).Scan(&count)
	if count != 0 {
		t.Fatal("published early")
	}
	if err := f.s.ProcessDue(context.Background(), now+61); err != nil {
		t.Fatal(err)
	}
	if err := f.s.ProcessDue(context.Background(), now+62); err != nil {
		t.Fatal(err)
	}
	f.db.QueryRow(`SELECT count(*) FROM messages WHERE content='later'`).Scan(&count)
	if count != 1 {
		t.Fatal("not exactly once", count)
	}
	b := f.call(t, 2, "POST", "/api/v1/me/scheduled-messages", input, 200)
	cancelled := number(t, b, "id")
	f.call(t, 2, "DELETE", fmt.Sprintf("/api/v1/me/scheduled-messages/%d", cancelled), nil, 200)
	c := f.call(t, 2, "POST", "/api/v1/me/scheduled-messages", input, 200)
	failed := number(t, c, "id")
	f.db.Exec(`DELETE FROM server_members WHERE server_id=1 AND user_id=2`)
	if err := f.s.ProcessDue(context.Background(), now+90); err != nil {
		t.Fatal(err)
	}
	var status string
	f.db.QueryRow(`SELECT status FROM scheduled_messages WHERE id=?`, failed).Scan(&status)
	if status != "failed" {
		t.Fatal(status)
	}
	f.db.QueryRow(`SELECT status FROM scheduled_messages WHERE id=?`, cancelled).Scan(&status)
	if status != "cancelled" {
		t.Fatal(status)
	}
}
func TestPollVotingAnonymityScopeAndClosing(t *testing.T) {
	f := setup(t)
	cid := number(t, f.call(t, 1, "POST", "/api/v1/me/conversations", map[string]any{"user_ids": []int64{2}}, 200), "id")
	poll := map[string]any{"question": "When?", "options": []string{"Today", "Tomorrow"}, "anonymous": true, "multiple": false, "closes_at": time.Now().Unix() + 3600}
	sent := f.call(t, 1, "POST", fmt.Sprintf("/api/v1/conversations/%d/messages", cid), map[string]any{"client_message_id": "poll-one", "poll": poll}, 200)
	var p message.Poll
	if err := json.Unmarshal(sent["poll"], &p); err != nil {
		t.Fatal(err)
	}
	if len(p.Options) != 2 {
		t.Fatal(p)
	}
	url := fmt.Sprintf("/api/v1/polls/%d/votes", p.ID)
	f.call(t, 2, "PUT", url, map[string]any{"option_ids": []int64{p.Options[0].ID, p.Options[1].ID}}, 400)
	f.call(t, 3, "PUT", url, map[string]any{"option_ids": []int64{p.Options[0].ID}}, 403)
	voted := f.call(t, 2, "PUT", url, map[string]any{"option_ids": []int64{p.Options[0].ID}}, 200)
	json.Unmarshal(voted["options"], &p.Options)
	if p.Options[0].Count != 1 || !p.Options[0].Me || len(p.Options[0].Voters) > 0 {
		t.Fatal(p)
	}
	f.call(t, 2, "PUT", url, map[string]any{"option_ids": []int64{p.Options[1].ID}}, 200)
	var count int
	f.db.QueryRow(`SELECT count(*) FROM poll_votes WHERE poll_id=?`, p.ID).Scan(&count)
	if count != 1 {
		t.Fatal(count)
	}
	f.db.Exec(`UPDATE polls SET closes_at=unixepoch()-1 WHERE id=?`, p.ID)
	f.call(t, 2, "PUT", url, map[string]any{"option_ids": []int64{}}, 409)
}
func TestEventRSVPReminderAndAuditPermissions(t *testing.T) {
	f := setup(t)
	now := time.Now().Unix()
	v := f.call(t, 1, "POST", "/api/v1/servers/1/events", map[string]any{"title": "Game night", "starts_at": now + 600, "timezone": "Europe/Moscow"}, 200)
	eid := number(t, v, "id")
	rsvp := fmt.Sprintf("/api/v1/servers/1/events/%d/response", eid)
	f.call(t, 3, "PUT", rsvp, map[string]any{"response": "going"}, 403)
	f.call(t, 2, "PUT", rsvp, map[string]any{"response": "going"}, 200)
	f.call(t, 2, "PUT", rsvp, map[string]any{"response": "maybe"}, 200)
	notices := 0
	f.s.notify = func(uid int64, event string, v any) {
		if uid == 2 && event == "community.event-reminder" {
			notices++
		}
	}
	if err := f.s.ProcessDue(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if err := f.s.ProcessDue(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if notices != 1 {
		t.Fatal("duplicate reminder", notices)
	}
	f.call(t, 2, "DELETE", fmt.Sprintf("/api/v1/servers/1/events/%d", eid), nil, 403)
	channels := channelsqlite.NewRepository(f.db)
	created, err := channels.Create(context.Background(), 1, 1, "Audit", channel.KindText)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = channels.Delete(context.Background(), 1, created.ID, 1); err != nil {
		t.Fatal(err)
	}
	f.call(t, 2, "GET", "/api/v1/servers/1/audit", nil, 403)
	result := f.call(t, 1, "GET", "/api/v1/servers/1/audit?action=channel.deleted&actor_id=1", nil, 200)
	var items []AuditEntry
	json.Unmarshal(result["items"], &items)
	if len(items) != 1 || items[0].TargetName != "Audit" || items[0].Actor != "owner" {
		t.Fatal(items)
	}
}
func TestCallSignalingMembershipSessionAndBlocks(t *testing.T) {
	f := setup(t)
	cid := number(t, f.call(t, 1, "POST", "/api/v1/me/conversations", map[string]any{"user_ids": []int64{2}}, 200), "id")
	url := fmt.Sprintf("/api/v1/conversations/%d/call", cid)
	one, two := "session-owner-00000001", "session-member-000001"
	f.call(t, 1, "PUT", url, map[string]any{"session_id": one}, 200)
	f.call(t, 2, "PUT", url, map[string]any{"session_id": two}, 200)
	f.call(t, 3, "GET", url, nil, 403)
	signal := map[string]any{"session_id": one, "target_user_id": 2, "target_session_id": two, "description": map[string]string{"type": "offer", "sdp": "test"}}
	emitted := 0
	f.s.notify = func(uid int64, event string, data any) {
		if event == "conversation.call-signal" {
			if uid != 2 {
				t.Fatal(uid)
			}
			emitted++
		}
	}
	f.call(t, 1, "POST", url+"/signal", signal, 200)
	if emitted != 1 {
		t.Fatal(emitted)
	}
	signal["target_session_id"] = "wrong"
	f.call(t, 1, "POST", url+"/signal", signal, 403)
	f.call(t, 1, "PUT", url, map[string]any{"session_id": "other-session-owner"}, 409)
	f.call(t, 2, "PUT", "/api/v1/me/blocks/1", nil, 200)
	signal["target_session_id"] = two
	f.call(t, 1, "POST", url+"/signal", signal, 403)
	f.call(t, 1, "PUT", url, map[string]any{"session_id": one}, 403)
	f.call(t, 1, "DELETE", url, map[string]any{"session_id": "wrong"}, 200)
	var count int
	f.db.QueryRow(`SELECT count(*) FROM conversation_call_members`).Scan(&count)
	if count != 2 {
		t.Fatal("stale leave removed active call")
	}
	f.call(t, 1, "DELETE", url, map[string]any{"session_id": one}, 200)
	f.db.QueryRow(`SELECT count(*) FROM conversation_call_members`).Scan(&count)
	if count != 1 {
		t.Fatal(count)
	}
	request := httptest.NewRequest("GET", url, nil)
	request.SetPathValue("conversationID", fmt.Sprint(cid))
	if _, err := f.s.callState(request, 3); err != ErrForbidden {
		t.Fatal(err)
	}
}

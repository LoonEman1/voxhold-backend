package community

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"voxhold-backend/internal/account"
	"voxhold-backend/internal/channel"
	channelsqlite "voxhold-backend/internal/channel/sqlite"
	"voxhold-backend/internal/message"
	messagesqlite "voxhold-backend/internal/message/sqlite"
	"voxhold-backend/internal/testdb"
)

type fixture struct {
	db  *sql.DB
	s   *Service
	mux *http.ServeMux
}

func setup(t *testing.T) fixture {
	t.Helper()
	db := testdb.Open(t)
	s := New(db, nil, nil)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux, func(h http.Handler) http.Handler { return h })
	return fixture{db, s, mux}
}
func (f fixture) call(t *testing.T, uid int64, method, url string, body any, code int) map[string]json.RawMessage {
	t.Helper()
	data, _ := json.Marshal(body)
	r := httptest.NewRequest(method, url, bytes.NewReader(data))
	r = r.WithContext(account.ContextWithUserID(r.Context(), uid))
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, r)
	if w.Code != code {
		t.Fatalf("%s %s: got %d want %d: %s", method, url, w.Code, code, w.Body.String())
	}
	var v map[string]json.RawMessage
	if w.Code == 200 && len(w.Body.Bytes()) > 0 && w.Body.Bytes()[0] == '{' {
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
	}
	return v
}
func TestConversationPrivacyBlocksReplayAndMembership(t *testing.T) {
	f := setup(t)
	f.call(t, 1, "POST", "/api/v1/me/conversations", map[string]any{"user_ids": []int64{3}}, 403)
	v := f.call(t, 1, "POST", "/api/v1/me/conversations", map[string]any{"user_ids": []int64{2}}, 200)
	var id int64
	json.Unmarshal(v["id"], &id)
	if id != 1 {
		t.Fatal(id)
	}
	f.call(t, 2, "POST", "/api/v1/me/conversations", map[string]any{"user_ids": []int64{1}}, 200)
	f.call(t, 3, "GET", "/api/v1/conversations/1/messages", nil, 403)
	payload := map[string]any{"content": "hello", "client_message_id": "same-key"}
	a := f.call(t, 1, "POST", "/api/v1/conversations/1/messages", payload, 200)
	b := f.call(t, 1, "POST", "/api/v1/conversations/1/messages", payload, 200)
	if string(a["id"]) != string(b["id"]) {
		t.Fatal("retry duplicated send")
	}
	payload["content"] = "different"
	f.call(t, 1, "POST", "/api/v1/conversations/1/messages", payload, 409)
	f.call(t, 2, "PUT", "/api/v1/me/blocks/1", nil, 200)
	payload["client_message_id"] = "blocked"
	f.call(t, 1, "POST", "/api/v1/conversations/1/messages", payload, 403)
	f.call(t, 2, "DELETE", "/api/v1/me/blocks/1", nil, 200)
	f.call(t, 2, "PUT", "/api/v1/me/message-privacy", map[string]string{"mode": "none"}, 200)
	f.call(t, 1, "POST", "/api/v1/conversations/1/messages", payload, 403)
	f.call(t, 2, "PUT", "/api/v1/me/message-privacy", map[string]string{"mode": "shared"}, 200)
	f.call(t, 2, "PUT", "/api/v1/conversations/1/read", map[string]int64{"message_id": 9999}, 400)
	f.call(t, 2, "DELETE", "/api/v1/conversations/1/members/me", nil, 200)
	f.call(t, 2, "GET", "/api/v1/conversations/1/messages", nil, 403)
	f.call(t, 1, "POST", "/api/v1/conversations/1/messages", payload, 403)
}
func TestRolesEnforceWriteReadVoiceAndModeration(t *testing.T) {
	f := setup(t)
	role := map[string]any{"name": "Readers", "color": "#123abc", "permissions": 1}
	f.call(t, 2, "POST", "/api/v1/servers/1/roles", role, 403)
	f.call(t, 1, "POST", "/api/v1/servers/1/roles", role, 200)
	f.call(t, 1, "PUT", "/api/v1/servers/1/members/2/roles", map[string]any{"role_ids": []int64{1}}, 200)
	ctx := context.Background()
	messages := messagesqlite.NewRepository(f.db)
	if _, err := messages.Create(ctx, 1, 2, 2, "forbidden"); err != message.ErrForbidden {
		t.Fatalf("write = %v", err)
	}
	channels := channelsqlite.NewRepository(f.db)
	if err := channels.CheckVoiceAccess(ctx, 1, 1, 2); err != channel.ErrForbidden {
		t.Fatalf("voice = %v", err)
	}
	role["permissions"] = 19
	f.call(t, 1, "PUT", "/api/v1/servers/1/roles/1", role, 200)
	post, err := messages.Create(ctx, 1, 2, 1, "moderate me")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = messages.Delete(ctx, 1, 2, post.ID, 2); err != nil {
		t.Fatal(err)
	}
	role["permissions"] = 0
	f.call(t, 1, "PUT", "/api/v1/servers/1/roles/1", role, 200)
	if err = channels.CheckAccess(ctx, 1, 2, 2); err != channel.ErrForbidden {
		t.Fatalf("read = %v", err)
	}
	// An invalid replacement rolls back and retains previous assignments.
	f.call(t, 1, "PUT", "/api/v1/servers/1/members/2/roles", map[string]any{"role_ids": []int64{999}}, 400)
	if err = channels.CheckAccess(ctx, 1, 2, 2); err != channel.ErrForbidden {
		t.Fatal("assignment rollback failed")
	}
	f.call(t, 1, "PUT", "/api/v1/servers/1/members/1/roles", map[string]any{"role_ids": []int64{1}}, 403)
}
func TestTextThreadsKeepQuotesSeparateAndInboxSurvivesChannelRead(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	repo := messagesqlite.NewRepository(f.db)
	root, err := repo.Create(ctx, 1, 2, 1, "root")
	if err != nil {
		t.Fatal(err)
	}
	quote, err := repo.CreateReferenced(ctx, 1, 2, 2, message.CreateInput{Content: "quote", ReplyToMessageID: &root.ID}, nil)
	if err != nil || quote.ThreadRootID != nil {
		t.Fatalf("quote: %+v %v", quote, err)
	}
	reply, err := repo.CreateReferenced(ctx, 1, 2, 2, message.CreateInput{Content: "thread", ThreadRootID: &root.ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.s.RecordThread(reply); err != nil {
		t.Fatal(err)
	}
	comments, err := repo.ListComments(ctx, 1, 2, 1, root.ID, nil, 20)
	if err != nil || len(comments) != 1 {
		t.Fatalf("comments: %v %v", comments, err)
	}
	r := httptest.NewRequest("GET", "/api/v1/me/inbox", nil)
	v, err := f.s.inbox(r, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.(map[string]any)["items"].([]InboxItem)) != 1 {
		t.Fatal(v)
	}
	if _, err = f.db.Exec(`INSERT INTO channel_reads(channel_id,user_id,last_read_message_id) VALUES(2,1,?)`, reply.ID); err != nil {
		t.Fatal(err)
	}
	v, err = f.s.inbox(r, 1)
	if err != nil || len(v.(map[string]any)["items"].([]InboxItem)) != 1 {
		t.Fatalf("channel read swallowed thread: %v %v", v, err)
	}
	f.call(t, 1, "PUT", "/api/v1/channels/2/threads/1", map[string]any{"following": true, "last_read_id": reply.ID}, 200)
	v, err = f.s.inbox(r, 1)
	if err != nil || len(v.(map[string]any)["items"].([]InboxItem)) != 0 {
		t.Fatalf("read thread: %v %v", v, err)
	}
}

func TestReadingDoesNotDisableAutoFollowAndExplicitOptOutSurvivesReplies(t *testing.T) {
	f := setup(t)
	repo := messagesqlite.NewRepository(f.db)
	ctx := context.Background()
	root, err := repo.Create(ctx, 1, 2, 1, "root")
	if err != nil {
		t.Fatal(err)
	}
	f.call(t, 2, "PUT", "/api/v1/channels/2/threads/1", map[string]any{"last_read_id": 0}, 200)
	reply, err := repo.CreateReferenced(ctx, 1, 2, 2, message.CreateInput{Content: "participate", ThreadRootID: &root.ID, ClientMessageID: "thread-retry"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.s.RecordThread(reply); err != nil {
		t.Fatal(err)
	}
	var following bool
	if err = f.db.QueryRow(`SELECT following FROM thread_subscriptions WHERE message_id=? AND user_id=2`, root.ID).Scan(&following); err != nil || !following {
		t.Fatalf("read disabled automatic following: %v %v", following, err)
	}
	f.call(t, 2, "PUT", "/api/v1/channels/2/threads/1", map[string]any{"following": false, "last_read_id": reply.ID}, 200)
	if err = f.s.RecordThread(reply); err != nil {
		t.Fatal(err)
	}
	if err = f.db.QueryRow(`SELECT following FROM thread_subscriptions WHERE message_id=? AND user_id=2`, root.ID).Scan(&following); err != nil || following {
		t.Fatalf("explicit opt-out lost: %v %v", following, err)
	}
	replay, err := repo.CreateReferenced(ctx, 1, 2, 2, message.CreateInput{Content: "participate", ThreadRootID: &root.ID, ClientMessageID: "thread-retry"}, nil)
	if err != nil || replay.ID != reply.ID || !replay.Replayed {
		t.Fatalf("thread retry: %+v %v", replay, err)
	}
}

func TestDirectAttachmentScopeAtomicBindingAndRetry(t *testing.T) {
	f := setup(t)
	events := 0
	f.s.notify = func(_ int64, event string, value any) {
		if event == "conversation.message" {
			m := value.(DirectMessage)
			if len(m.Attachments) != 1 || m.Attachments[0].ID != "mine" {
				t.Errorf("event omitted attachments: %+v", m)
			}
			events++
		}
	}
	f.call(t, 1, "POST", "/api/v1/me/conversations", map[string]any{"user_ids": []int64{2}}, 200)
	_, err := f.db.Exec(`INSERT INTO attachments(id,conversation_id,owner_user_id,filename,mime_type,category,size_bytes) VALUES('mine',1,1,'mine.txt','text/plain','files',4),('theirs',1,2,'theirs.txt','text/plain','files',6)`)
	if err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{"content": "", "client_message_id": "private-file", "attachment_ids": []string{"mine", "theirs"}}
	f.call(t, 1, "POST", "/api/v1/conversations/1/messages", payload, 400)
	var count int
	var bound sql.NullInt64
	if err = f.db.QueryRow(`SELECT count(*) FROM conversation_messages`).Scan(&count); err != nil || count != 0 {
		t.Fatal("partial message committed", count, err)
	}
	if err = f.db.QueryRow(`SELECT direct_message_id FROM attachments WHERE id='mine'`).Scan(&bound); err != nil || bound.Valid {
		t.Fatal("partial binding committed", bound, err)
	}
	payload["attachment_ids"] = []string{"mine"}
	first := f.call(t, 1, "POST", "/api/v1/conversations/1/messages", payload, 200)
	again := f.call(t, 1, "POST", "/api/v1/conversations/1/messages", payload, 200)
	for _, response := range []map[string]json.RawMessage{first, again} {
		var attachments []message.Attachment
		if err := json.Unmarshal(response["attachments"], &attachments); err != nil || len(attachments) != 1 || attachments[0].ID != "mine" || attachments[0].Deleted {
			t.Fatalf("send/retry omitted attachment metadata: %s (%v)", response["attachments"], err)
		}
	}
	if string(first["id"]) != string(again["id"]) || events != 2 {
		t.Fatal("replayed file notified twice", events)
	}
	r := httptest.NewRequest("GET", "/api/v1/conversations/1/messages", nil)
	r.SetPathValue("conversationID", "1")
	page, err := f.s.conversationMessages(r, 2)
	if err != nil {
		t.Fatal(err)
	}
	messages := page.(map[string]any)["messages"].([]DirectMessage)
	if len(messages) != 1 || len(messages[0].Attachments) != 1 || messages[0].Attachments[0].ID != "mine" {
		t.Fatal(messages)
	}
}

func TestDirectRetryIncludesServerDeletedAttachmentPlaceholder(t *testing.T) {
	f := setup(t)
	f.call(t, 1, "POST", "/api/v1/me/conversations", map[string]any{"user_ids": []int64{2}}, 200)
	if _, err := f.db.Exec(`INSERT INTO attachments(id,conversation_id,owner_user_id,filename,mime_type,category,size_bytes) VALUES('deleted-file',1,1,'gone.txt','text/plain','files',4)`); err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{"content": "caption", "client_message_id": "deleted-retry", "attachment_ids": []string{"deleted-file"}}
	f.call(t, 1, "POST", "/api/v1/conversations/1/messages", payload, 200)
	if _, err := f.db.Exec(`UPDATE attachments SET deleted_at=1 WHERE id='deleted-file'`); err != nil {
		t.Fatal(err)
	}
	replay := f.call(t, 1, "POST", "/api/v1/conversations/1/messages", payload, 200)
	var attachments []message.Attachment
	if err := json.Unmarshal(replay["attachments"], &attachments); err != nil || len(attachments) != 1 || !attachments[0].Deleted || attachments[0].Filename != "gone.txt" {
		t.Fatalf("retry lost deleted placeholder: %s (%v)", replay["attachments"], err)
	}
}

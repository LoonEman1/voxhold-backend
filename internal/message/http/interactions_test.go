package messagehttp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"voxhold-backend/internal/account"
	"voxhold-backend/internal/message"
	messagesqlite "voxhold-backend/internal/message/sqlite"
	"voxhold-backend/internal/testdb"
)

type interactionEvents struct{ message.EventPublisher }

func (interactionEvents) PublishMessageCreated(message.Message) {}

func TestInteractionHTTPAuthorizationReplayAndPrivacy(t *testing.T) {
	db := testdb.Open(t)
	repo := messagesqlite.NewRepository(db)
	original, err := repo.Create(context.Background(), 1, 2, 1, "hello <@2>")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	NewHandler(message.NewService(repo, interactionEvents{})).RegisterRoutes(mux, func(h http.Handler) http.Handler { return h })
	request := func(method, path, body string, user int64, status int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if user > 0 {
			r = r.WithContext(account.ContextWithUserID(r.Context(), user))
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: %d want %d: %s", method, path, w.Code, status, w.Body.String())
		}
		return w
	}
	base := "/api/v1/servers/1/channels/2/messages"
	reaction := fmt.Sprintf("%s/%d/reactions/%s", base, original.ID, url.PathEscape("❤️"))
	request("PUT", reaction, "", 3, 403)
	first := request("PUT", reaction, "", 2, 200)
	if first.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("personalized response must not be cached")
	}
	var state message.ReactionsData
	if err := json.Unmarshal(first.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	again := request("PUT", reaction, "", 2, 200)
	var replay message.ReactionsData
	_ = json.Unmarshal(again.Body.Bytes(), &replay)
	if len(state.Reactions) != 1 || !state.Reactions[0].Me || state.Reactions[0].Count != 1 || replay.Revision != state.Revision {
		t.Fatalf("non-idempotent reaction: %s", again.Body)
	}
	request("GET", reaction+"?after_user_id=-1", "", 2, 400)
	request("GET", reaction, "", 3, 403)
	people := request("GET", reaction, "", 1, 200)
	if !strings.Contains(people.Body.String(), `"username":"member"`) {
		t.Fatal(people.Body.String())
	}
	request("PUT", fmt.Sprintf("%s/%d/reactions/unsupported", base, original.ID), "", 2, 400)
	request("DELETE", reaction, "", 2, 200)
	request("DELETE", reaction, "", 2, 200)
	reply := request("POST", base, fmt.Sprintf(`{"content":"answer","reply_to_message_id":%d,"client_message_id":"http-reply"}`, original.ID), 2, 201)
	var quoted struct{ Reply *message.ReplyReference }
	if err := json.Unmarshal(reply.Body.Bytes(), &quoted); err != nil {
		t.Fatal(err)
	}
	if quoted.Reply == nil || quoted.Reply.Content != "hello <@2>" {
		t.Fatal(reply.Body.String())
	}
	request("POST", base, `{"content":"answer","reply_to_message_id":999}`, 2, 400)
	request("POST", base+"/forward", `{}`, 2, 400)
	body := fmt.Sprintf(`{"source_server_id":1,"source_channel_id":2,"source_message_id":%d,"client_message_id":"http-forward"}`, original.ID)
	forwarded := request("POST", "/api/v1/servers/1/channels/4/messages/forward", body, 2, 201)
	var value struct {
		ID      int64
		Forward map[string]any
	}
	if err := json.Unmarshal(forwarded.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"source_server_id", "source_channel_id", "source_message_id"} {
		if _, found := value.Forward[key]; found {
			t.Fatalf("private source leaked: %s", forwarded.Body)
		}
	}
	if value.Forward["author"] == nil {
		t.Fatal("missing original author")
	}
	request("PATCH", fmt.Sprintf("/api/v1/servers/1/channels/4/messages/%d", value.ID), `{"content":"forged"}`, 2, 403)
	again = request("POST", "/api/v1/servers/1/channels/4/messages/forward", body, 2, 201)
	if again.Body.String() != forwarded.Body.String() {
		t.Fatalf("retry changed snapshot: %s", again.Body)
	}
}

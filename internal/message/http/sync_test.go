package messagehttp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"voxhold-backend/internal/account"
	"voxhold-backend/internal/message"
	messagesqlite "voxhold-backend/internal/message/sqlite"
	"voxhold-backend/internal/realtime"
	"voxhold-backend/internal/testdb"
)

func TestHTTPMessageRetryAndSyncContract(t *testing.T) {
	service := message.NewService(messagesqlite.NewRepository(testdb.Open(t)), realtime.NewMessageEventPublisher(realtime.NewHub()))
	mux := http.NewServeMux()
	NewHandler(service).RegisterRoutes(mux, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := int64(1)
			if r.Header.Get("X-Outsider") != "" {
				userID = 3
			}
			next.ServeHTTP(w, r.WithContext(account.ContextWithUserID(r.Context(), userID)))
		})
	})
	request := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	const base = "/api/v1/servers/1/channels/2/messages"
	const body = `{"content":"hello","client_message_id":"stable-key"}`
	first := request("POST", base, body)
	if first.Code != 201 {
		t.Fatal(first.Code, first.Body.String())
	}
	var created messageResponse
	if err := json.Unmarshal(first.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	second := request("POST", base, body)
	if second.Code != 201 || second.Body.String() != first.Body.String() || created.ClientMessageID != "stable-key" {
		t.Fatal(first.Body, second.Code, second.Body)
	}
	if response := request("POST", base, `{"content":"different","client_message_id":"stable-key"}`); response.Code != 409 {
		t.Fatal(response.Code, response.Body)
	}
	if response := request("GET", base+"/sync?after=-1", ""); response.Code != 400 {
		t.Fatal(response.Code, response.Body)
	}
	if response := request("GET", base+"/sync", ""); response.Code != 200 || !strings.Contains(response.Body.String(), `"reset":true`) {
		t.Fatal(response.Code, response.Body)
	}
	if response := request("DELETE", fmt.Sprintf("%s/%d", base, created.ID), ""); response.Code != 204 {
		t.Fatal(response.Code, response.Body)
	}
	if response := request("POST", base, body); response.Code != 410 {
		t.Fatal(response.Code, response.Body)
	}
	r := httptest.NewRequest("GET", base+"/sync", nil)
	r.Header.Set("X-Outsider", "1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code, w.Body)
	}
}

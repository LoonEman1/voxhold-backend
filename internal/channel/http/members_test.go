package channelhttp

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"voxhold-backend/internal/account"
	"voxhold-backend/internal/channel"
	channelsqlite "voxhold-backend/internal/channel/sqlite"
	"voxhold-backend/internal/testdb"
)

func TestChannelAccessRoster(t *testing.T) {
	db := testdb.Open(t)
	service := channel.NewService(channelsqlite.NewRepository(db), nil)
	mux := http.NewServeMux()
	NewHandler(service).RegisterRoutes(mux, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, _ := strconv.ParseInt(r.Header.Get("X-Test-User"), 10, 64)
			if id <= 0 {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r.WithContext(account.ContextWithUserID(r.Context(), id)))
		})
	})
	request := func(path string, userID int64) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("X-Test-User", strconv.FormatInt(userID, 10))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	for _, channelID := range []int{1, 2} {
		path := "/api/v1/servers/1/channels/" + strconv.Itoa(channelID) + "/members"
		for _, userID := range []int64{1, 2} {
			w := request(path, userID)
			if w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"user_ids":[1,2]}` || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal(w.Code, w.Body)
			}
		}
		if w := request(path, 3); w.Code != 403 {
			t.Fatal(w.Code, w.Body)
		}
		if w := request(path, 0); w.Code != 401 {
			t.Fatal(w.Code, w.Body)
		}
	}
	for _, path := range []string{"/api/v1/servers/1/channels/99/members", "/api/v1/servers/99/channels/1/members"} {
		if w := request(path, 1); w.Code != 404 {
			t.Fatal(w.Code, w.Body)
		}
	}
	if _, err := db.Exec("DELETE FROM server_members WHERE server_id=1 AND user_id=2"); err != nil {
		t.Fatal(err)
	}
	w := request("/api/v1/servers/1/channels/1/members", 1)
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"user_ids":[1]}` {
		t.Fatal(w.Code, w.Body)
	}
	if w := request("/api/v1/servers/1/channels/1/members", 2); w.Code != 403 {
		t.Fatal(w.Code, w.Body)
	}
}

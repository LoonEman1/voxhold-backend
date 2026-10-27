package channelhttp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"voxhold-backend/internal/account"
	"voxhold-backend/internal/channel"
	channelsqlite "voxhold-backend/internal/channel/sqlite"
	"voxhold-backend/internal/testdb"
)

func TestAccessSettingsRequireManagerAndCompleteValidPolicy(t *testing.T) {
	db := testdb.Open(t)
	service := channel.NewService(channelsqlite.NewRepository(db), nil)
	mux := http.NewServeMux()
	NewHandler(service).RegisterRoutes(mux, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if id, ok := account.UserIDFromContext(r.Context()); !ok || id <= 0 {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	request := func(method, path, body string, userID int64) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r = r.WithContext(account.ContextWithUserID(r.Context(), userID))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	path := "/api/v1/servers/1/channels/2/access"
	for _, test := range []struct {
		method, path, body string
		user               int64
		status             int
	}{
		{"GET", path, "", 0, 401}, {"GET", path, "", 2, 403}, {"PUT", path, `{"restricted":true}`, 2, 403},
		{"PUT", path, `{}`, 1, 400}, {"PUT", path, `{"restricted":null}`, 1, 400},
		{"PUT", path, `{"restricted":true,"roles":["bogus"]}`, 1, 400},
		{"PUT", path, `{"restricted":true,"user_ids":[3]}`, 1, 400},
		{"PUT", path, `{"restricted":true,"user_ids":[2,2]}`, 1, 200},
		{"GET", path, "", 1, 200}, {"GET", "/api/v1/servers/99/channels/2/access", "", 1, 404},
	} {
		w := request(test.method, test.path, test.body, test.user)
		if w.Code != test.status {
			t.Fatalf("%+v: %d %s", test, w.Code, w.Body)
		}
		if w.Code == 200 && w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("ACL response cacheable")
		}
	}
}

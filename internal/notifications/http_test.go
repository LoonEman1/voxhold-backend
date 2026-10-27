package notifications

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"voxhold-backend/internal/account"
	"voxhold-backend/internal/testdb"
)

func TestNotificationPreferencesHTTP(t *testing.T) {
	service := NewService(testdb.Open(t), nil, nil, nil)
	mux := http.NewServeMux()
	service.RegisterRoutes(mux, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, _ := strconv.ParseInt(r.Header.Get("X-Test-User"), 10, 64)
			if id <= 0 {
				w.WriteHeader(401)
				return
			}
			next.ServeHTTP(w, r.WithContext(account.ContextWithUserID(r.Context(), id)))
		})
	})
	for _, test := range []struct {
		method, path, body string
		user, status       int
	}{
		{"GET", "/api/v1/servers/1/channels/2/notifications", "", 2, 200},
		{"GET", "/api/v1/servers/1/channels/2/notifications", "", 0, 401},
		{"GET", "/api/v1/servers/1/channels/2/notifications", "", 3, 403},
		{"PUT", "/api/v1/servers/1/channels/2/notifications", `{"mode":"all"}`, 2, 200},
		{"PUT", "/api/v1/servers/1/channels/2/notifications", `{"mode":"none"}`, 2, 200},
		{"PUT", "/api/v1/servers/1/channels/2/notifications", `{}`, 2, 400},
		{"PUT", "/api/v1/servers/1/channels/2/notifications", `{"mode":"bogus"}`, 2, 400},
		{"PUT", "/api/v1/servers/1/channels/2/notifications", `{"mode":"all","user_id":1}`, 2, 400},
		{"GET", "/api/v1/servers/99/channels/2/notifications", "", 2, 403},
		{"GET", "/api/v1/servers/1/channels/1/notifications", "", 2, 400},
	} {
		r := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
		r.Header.Set("X-Test-User", strconv.Itoa(test.user))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != test.status {
			t.Fatalf("%+v: %d %s", test, w.Code, w.Body)
		}
		if w.Code == 200 && w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("preferences cached")
		}
	}
}

package invitehttp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"voxhold-backend/internal/account"
	"voxhold-backend/internal/invite"
	invitesqlite "voxhold-backend/internal/invite/sqlite"
	"voxhold-backend/internal/testdb"
)

func TestManagedInviteHTTPContractAndAuthorization(t *testing.T) {
	db := testdb.Open(t)
	if _, err := db.Exec(`INSERT INTO users(id,username,password_hash) VALUES(4,'admin','hash'); INSERT INTO server_members(server_id,user_id,role) VALUES(1,4,'admin')`); err != nil {
		t.Fatal(err)
	}
	service := invite.NewService(invitesqlite.NewRepository(db), nil, nil, nil)
	limit := 5
	first, err := service.CreateLink(context.Background(), 1, 1, invite.CreateLinkInput{Lifetime: time.Hour, MaxUses: &limit, AllowRegistration: true})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.CreateLink(context.Background(), 1, 4, invite.CreateLinkInput{Lifetime: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	NewHandler(service).RegisterRoutes(mux, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, _ := strconv.ParseInt(r.Header.Get("X-Test-User"), 10, 64)
			if id <= 0 {
				w.WriteHeader(401)
				return
			}
			next.ServeHTTP(w, r.WithContext(account.ContextWithUserID(r.Context(), id)))
		})
	})
	request := func(method, path string, userID int64, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("X-Test-User", strconv.FormatInt(userID, 10))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	const base = "/api/v1/servers/1/invite-links"
	for _, id := range []int64{1, 4} {
		response := request("GET", base+"?limit=1", id, "")
		if response.Code != 200 || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(response.Code, response.Body)
		}
		if strings.Contains(response.Body.String(), "token") || strings.Contains(response.Body.String(), first.Token) {
			t.Fatal("management response exposed token material")
		}
		var body struct {
			Links      []managedLinkResponse `json:"invite_links"`
			Pagination struct {
				Before *int64 `json:"next_before_id"`
				More   bool   `json:"has_more"`
			} `json:"pagination"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if len(body.Links) != 1 || body.Links[0].ID != second.ID || !body.Pagination.More || body.Pagination.Before == nil {
			t.Fatal(body)
		}
		response = request("GET", fmt.Sprintf("%s?before_id=%d", base, *body.Pagination.Before), id, "")
		if response.Code != 200 || !strings.Contains(response.Body.String(), `"created_by":1`) {
			t.Fatal(response.Code, response.Body)
		}
	}
	for _, id := range []int64{0, 2, 3} {
		want := 403
		if id == 0 {
			want = 401
		}
		if response := request("GET", base, id, ""); response.Code != want {
			t.Fatal(id, response.Code, response.Body)
		}
		if response := request("DELETE", fmt.Sprintf("%s/%d", base, first.ID), id, ""); response.Code != want {
			t.Fatal(id, response.Code, response.Body)
		}
	}
	for _, query := range []string{"?limit=0", "?limit=101", "?before_id=-1", "?before_id=oops"} {
		if response := request("GET", base+query, 1, ""); response.Code != 400 {
			t.Fatal(query, response.Code, response.Body)
		}
	}
	for _, id := range []int64{4, 1} {
		if response := request("DELETE", fmt.Sprintf("%s/%d", base, first.ID), id, ""); response.Code != 204 {
			t.Fatal(response.Code, response.Body)
		}
	}
	if response := request("POST", "/api/v1/invite-links/resolve", 0, fmt.Sprintf(`{"token":%q}`, first.Token)); response.Code != 404 {
		t.Fatal(response.Code, response.Body)
	}
	if response := request("DELETE", base+"/999999", 1, ""); response.Code != 404 {
		t.Fatal(response.Code, response.Body)
	}
}

package http

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"voxhold-backend/internal/account"
)

func fetchAs(t *testing.T, handler *Handler, userID int64) configResponse {
	t.Helper()
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, func(next http.Handler) http.Handler { return next })
	request := httptest.NewRequest(http.MethodGet, "/api/v1/webrtc/config", nil)
	request = request.WithContext(account.ContextWithUserID(context.Background(), userID))
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	var out configResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRESTCredentialsAreTimeLimitedAndPerUser(t *testing.T) {
	fixed := time.Unix(1_700_000_000, 0)
	handler := NewHandler([]string{"turn:turn.example:3478"}, "static", "static-pass").WithTURNSecret("secret", 12*time.Hour)
	handler.now = func() time.Time { return fixed }

	a := fetchAs(t, handler, 7)
	b := fetchAs(t, handler, 8)
	expires := fixed.Add(12 * time.Hour).Unix()
	if a.CredentialExpiresAt != expires {
		t.Fatalf("expires=%d want %d", a.CredentialExpiresAt, expires)
	}
	wantName := strconv.FormatInt(expires, 10) + ":7"
	if got := a.ICEServers[0].Username; got != wantName {
		t.Fatalf("username=%q want %q", got, wantName)
	}
	mac := hmac.New(sha1.New, []byte("secret"))
	mac.Write([]byte(wantName))
	if want := base64.StdEncoding.EncodeToString(mac.Sum(nil)); a.ICEServers[0].Credential != want {
		t.Fatalf("credential mismatch")
	}
	if a.ICEServers[0].Credential == b.ICEServers[0].Credential {
		t.Fatal("different users share credentials")
	}
}

func TestStaticCredentialsWithoutSecretHaveNoExpiry(t *testing.T) {
	out := fetchAs(t, NewHandler([]string{"turn:x:3478"}, "u", "p"), 1)
	if out.CredentialExpiresAt != 0 || out.ICEServers[0].Username != "u" {
		t.Fatalf("unexpected: %+v", out)
	}
}

func TestRESTCredentialKnownVector(t *testing.T) {
	// HMAC-SHA1("key", "1:1") base64.
	name, cred := restCredential([]byte("key"), 1, 1)
	mac := hmac.New(sha1.New, []byte("key"))
	mac.Write([]byte("1:1"))
	if name != "1:1" || cred != base64.StdEncoding.EncodeToString(mac.Sum(nil)) {
		t.Fatalf("%q %q", name, cred)
	}
}

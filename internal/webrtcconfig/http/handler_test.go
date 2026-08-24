package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func stubRequireAuth(next http.Handler) http.Handler {
	return next
}

func serveConfig(
	t *testing.T,
	urls []string,
	username string,
	credential string,
) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	handler := NewHandler(urls, username, credential)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, stubRequireAuth)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/webrtc/config", nil)
	mux.ServeHTTP(recorder, request)

	var payload struct {
		ICEServers []struct {
			URLs       []string `json:"urls"`
			Username   string   `json:"username"`
			Credential string   `json:"credential"`
		} `json:"ice_servers"`
		ICETransportPolicy string `json:"ice_transport_policy"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	wrapped := map[string]any{
		"servers":  len(payload.ICEServers),
		"policy":   payload.ICETransportPolicy,
		"payload":  payload,
	}
	return recorder, wrapped
}

func TestConfigRequiresAuthorization(t *testing.T) {
	handler := NewHandler(nil, "", "")
	mux := http.NewServeMux()
	denied := true
	requireAuth := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if denied {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
	handler.RegisterRoutes(mux, requireAuth)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/webrtc/config", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", recorder.Code)
	}
}

func TestConfigEmptyListIsValid(t *testing.T) {
	recorder, payload := serveConfig(t, nil, "", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	if payload["servers"].(int) != 0 {
		t.Fatalf("expected empty ice_servers, got %v", payload["payload"])
	}
	if policy := payload["policy"].(string); policy != "all" {
		t.Fatalf("expected ice_transport_policy all, got %q", policy)
	}
	if header := recorder.Header().Get("Cache-Control"); header != "no-store" {
		t.Fatalf("expected Cache-Control no-store, got %q", header)
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("expected JSON content type, got %q", contentType)
	}
}

func TestConfigURLsWithCredentials(t *testing.T) {
	recorder, payload := serveConfig(
		t,
		[]string{"turn:turn.example.com:3478?transport=udp", "turn:turn.example.com:3478?transport=tcp"},
		"user",
		"secret",
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	servers := payload["payload"].(struct {
		ICEServers []struct {
			URLs       []string `json:"urls"`
			Username   string   `json:"username"`
			Credential string   `json:"credential"`
		} `json:"ice_servers"`
		ICETransportPolicy string `json:"ice_transport_policy"`
	}).ICEServers
	if len(servers) != 1 {
		t.Fatalf("expected one ICE server, got %d", len(servers))
	}
	if len(servers[0].URLs) != 2 {
		t.Fatalf("expected two URLs, got %v", servers[0].URLs)
	}
	if servers[0].Username != "user" || servers[0].Credential != "secret" {
		t.Fatalf("unexpected credentials in response: %+v", servers[0])
	}
}

func TestConfigOmitsEmptyCredentialFields(t *testing.T) {
	recorder, payload := serveConfig(t, []string{"turn:turn.example.com:3478"}, "", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	body := recorder.Body.String()
	if contains(body, "username") || contains(body, "credential") {
		t.Fatalf("empty credential fields must be omitted: %s", body)
	}
	if payload["servers"].(int) != 1 {
		t.Fatalf("expected one ICE server, got %v", body)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

func TestConfigCopiesInputSlice(t *testing.T) {
	urls := []string{"turn:turn.example.com:3478"}
	handler := NewHandler(urls, "", "")
	urls[0] = "turn:mutated.example.com:3478"
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, stubRequireAuth)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/webrtc/config", nil))
	if contains(recorder.Body.String(), "mutated") {
		t.Fatalf("handler must copy the input slice, got %s", recorder.Body.String())
	}
}

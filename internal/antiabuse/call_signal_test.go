package antiabuse

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCallSignalingHasBoundedIndependentBudget(t *testing.T) {
	config := DefaultConfig()
	config.HTTPRequestsPerSecond = 1
	config.HTTPBurst = 1
	config.HTTPWritesPerSecond = 1
	config.HTTPWriteBurst = 1
	config.CallSignalsPerSecond = 1
	config.CallSignalBurst = 3
	handler := New(config).ProtectHTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	request := func(path string, want int) {
		t.Helper()
		r := httptest.NewRequest("POST", path, nil)
		r.RemoteAddr = "192.0.2.44:1234"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s: %d want %d", path, w.Code, want)
		}
	}
	for range 3 {
		request("/api/v1/conversations/1/call/signal", 204)
	}
	request("/api/v1/conversations/1/call/signal", 429)
	request("/api/v1/conversations/1/messages", 204)
	request("/api/v1/conversations/1/messages", 429)
}
func TestCallSignalClassificationRejectsOtherRoutes(t *testing.T) {
	for _, path := range []string{"/api/v1/conversations/1/messages", "/api/v1/conversations/x/call/signal", "/api/v1/conversations/-1/call/signal", "/api/v1/conversations/1/call/signal/extra", "/api/v1/conversations/1/2/call/signal"} {
		if conversationCallSignal(httptest.NewRequest("POST", path, nil)) {
			t.Fatal(path)
		}
	}
	if conversationCallSignal(httptest.NewRequest("PUT", "/api/v1/conversations/1/call/signal", nil)) {
		t.Fatal("unexpected method")
	}
}

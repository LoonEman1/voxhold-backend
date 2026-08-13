package antiabuse

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProtectHTTPRateLimitsRequestsByClientIP(t *testing.T) {
	config := DefaultConfig()
	config.HTTPRequestsPerSecond = 1
	config.HTTPBurst = 1
	guard := New(config)

	handler := guard.ProtectHTTP(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		},
	))

	first := httptest.NewRecorder()
	firstRequest := httptest.NewRequest(http.MethodGet, "/missing", nil)
	firstRequest.RemoteAddr = "192.0.2.10:1234"
	handler.ServeHTTP(first, firstRequest)
	if first.Code != http.StatusNoContent {
		t.Fatalf("first request status = %d", first.Code)
	}

	second := httptest.NewRecorder()
	secondRequest := httptest.NewRequest(http.MethodGet, "/missing", nil)
	secondRequest.RemoteAddr = "192.0.2.10:4321"
	handler.ServeHTTP(second, secondRequest)
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second request status = %d", second.Code)
	}
	if second.Header().Get("Retry-After") == "" {
		t.Fatal("Retry-After header is missing")
	}
}

func TestTrustedProxyUsesOnlyValidatedRealIP(t *testing.T) {
	config := DefaultConfig()
	config.TrustProxyHeaders = true
	guard := New(config)

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "172.20.0.4:1234"
	request.Header.Set("X-Real-IP", "203.0.113.9")
	request.Header.Set("X-Forwarded-For", "198.51.100.7")

	if got := guard.resolveClientIP(request); got != "203.0.113.9" {
		t.Fatalf("client IP = %q", got)
	}

	request.Header.Set("X-Real-IP", "not-an-ip")
	if got := guard.resolveClientIP(request); got != "172.20.0.4" {
		t.Fatalf("fallback client IP = %q", got)
	}
}

func TestLoginFailuresTemporarilyBlockClient(t *testing.T) {
	config := DefaultConfig()
	config.LoginMaxFailures = 2
	config.LoginBlockDuration = 25 * time.Millisecond
	guard := New(config)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	request.RemoteAddr = "192.0.2.11:1234"

	guard.RecordLoginFailure(request, "owner")
	if allowed, _ := guard.AllowLogin(request, "owner"); !allowed {
		t.Fatal("login was blocked before reaching failure threshold")
	}

	guard.RecordLoginFailure(request, "owner")
	if allowed, retryAfter := guard.AllowLogin(request, "owner"); allowed || retryAfter <= 0 {

		t.Fatalf(
			"blocked login = (%v, %v)",
			allowed,
			retryAfter,
		)
	}

	time.Sleep(30 * time.Millisecond)
	if allowed, _ := guard.AllowLogin(request, "owner"); !allowed {
		t.Fatal("login remained blocked after block duration")
	}
}

func TestWebSocketConnectionLimitsAreReleased(t *testing.T) {
	config := DefaultConfig()
	config.WebSocketMaxPerIP = 1
	config.WebSocketMaxPerUser = 1
	guard := New(config)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/ws", nil)
	request.RemoteAddr = "192.0.2.12:1234"

	releaseIP, ok := guard.AcquireWebSocketIP(request)
	if !ok {
		t.Fatal("first IP connection was rejected")
	}
	if _, ok := guard.AcquireWebSocketIP(request); ok {
		t.Fatal("second IP connection was accepted")
	}
	releaseIP()
	if _, ok := guard.AcquireWebSocketIP(request); !ok {
		t.Fatal("IP connection slot was not released")
	}

	releaseUser, ok := guard.AcquireWebSocketUser(42)
	if !ok {
		t.Fatal("first user connection was rejected")
	}
	if _, ok := guard.AcquireWebSocketUser(42); ok {
		t.Fatal("second user connection was accepted")
	}
	releaseUser()
	if _, ok := guard.AcquireWebSocketUser(42); !ok {
		t.Fatal("user connection slot was not released")
	}
}

func TestWebSocketEventLimiterRejectsBurstOverflow(t *testing.T) {
	config := DefaultConfig()
	config.WebSocketEventsPerSecond = 1
	config.WebSocketEventBurst = 2
	guard := New(config)
	allow := guard.NewWebSocketEventLimiter()

	if !allow() || !allow() {
		t.Fatal("allowed event burst was rejected")
	}
	if allow() {
		t.Fatal("event above burst was accepted")
	}
}

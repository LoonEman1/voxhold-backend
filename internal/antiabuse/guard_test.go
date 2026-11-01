package antiabuse

import (
	"fmt"
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

func TestLoginFailureWindowExpiresBelowStoreCapacity(t *testing.T) {
	config := DefaultConfig()
	config.LoginMaxFailures = 2
	guard := New(config)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	request.RemoteAddr = "192.0.2.11:1234"
	guard.RecordLoginFailure(request, "owner")
	guard.failuresMu.Lock()
	for key, entry := range guard.failures {
		entry.lastSeen = time.Now().Add(-2 * config.EntryTTL)
		guard.failures[key] = entry
	}
	guard.failuresMu.Unlock()
	guard.RecordLoginFailure(request, "owner")
	if allowed, _ := guard.AllowLogin(request, "owner"); !allowed {
		t.Fatal("expired failures were counted toward a new block")
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

func TestIPv6ClientsShareBucketWithinPrefix(t *testing.T) {
	config := DefaultConfig()
	config.HTTPRequestsPerSecond = 1
	config.HTTPBurst = 1
	handler := New(config).ProtectHTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	status := func(remote string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.RemoteAddr = remote
		handler.ServeHTTP(rec, req)
		return rec.Code
	}
	if got := status("[2001:db8:1:1::1]:1"); got != http.StatusNoContent {
		t.Fatalf("first = %d", got)
	}
	if got := status("[2001:db8:1:1:ffff::2]:1"); got != http.StatusTooManyRequests {
		t.Fatalf("same /64 = %d", got)
	}
	if got := status("[2001:db8:1:2::1]:1"); got != http.StatusNoContent {
		t.Fatalf("other /64 = %d", got)
	}
}

func TestRateKeyKeepsIPv4(t *testing.T) {
	g := New(DefaultConfig())
	if got := g.rateKey("192.0.2.1"); got != "192.0.2.1" {
		t.Fatalf("got %q", got)
	}
}

func TestInvalidIPv6PrefixIsConfigError(t *testing.T) {
	t.Setenv("RATE_LIMIT_IPV6_PREFIX", "16")
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("expected error")
	}
	t.Setenv("RATE_LIMIT_IPV6_PREFIX", "56")
	if c, err := ConfigFromEnv(); err != nil || c.IPv6PrefixBits != 56 {
		t.Fatalf("got %v %v", c.IPv6PrefixBits, err)
	}
}

func TestFullLimiterTableEvictsInsteadOfRejecting(t *testing.T) {
	store := limiterStore{entries: map[string]*limiterEntry{}, ttl: time.Hour, maxEntries: 3}
	for _, key := range []string{"a", "b", "c", "d"} {
		if !store.allow(key, 10, 10) {
			t.Fatalf("key %q rejected", key)
		}
	}
	if len(store.entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(store.entries))
	}
}

func TestLoginBlockWorksWhenFailureTableIsFull(t *testing.T) {
	config := DefaultConfig()
	config.MaxRateEntries = 40
	config.LoginMaxFailures = 2
	guard := New(config)
	for i := 0; i < 100; i++ {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.RemoteAddr = fmt.Sprintf("198.51.100.%d:1", i)
		guard.RecordLoginFailure(r, fmt.Sprintf("user%d", i))
	}
	victim := httptest.NewRequest(http.MethodPost, "/", nil)
	victim.RemoteAddr = "203.0.113.5:1"
	guard.RecordLoginFailure(victim, "target")
	guard.RecordLoginFailure(victim, "target")
	if ok, _ := guard.AllowLogin(victim, "target"); ok {
		t.Fatal("login should be blocked after repeated failures")
	}
}

func TestAuthenticatedUsersBehindOneIPHaveSeparateBudgets(t *testing.T) {
	config := DefaultConfig()
	config.HTTPRequestsPerSecond = 1
	config.HTTPBurst = 1
	if config.IPMultiplier != 4 {
		t.Fatalf("default HTTP_IP_MULTIPLIER = %d, want 4", config.IPMultiplier)
	}
	handler := New(config).ProtectHTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	status := func(token string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.RemoteAddr = "192.0.2.50:1"
		req.Header.Set("Authorization", "Bearer "+token)
		handler.ServeHTTP(rec, req)
		return rec.Code
	}
	if status("alice") != http.StatusNoContent || status("bob") != http.StatusNoContent {
		t.Fatal("two users behind one IP must each get a request")
	}
	if status("alice") != http.StatusTooManyRequests {
		t.Fatal("a user must still be limited by their own budget")
	}
	for _, token := range []string{"c", "d", "e"} {
		status(token)
	}
	if status("f") != http.StatusTooManyRequests {
		t.Fatal("the outer IP budget must still apply to many fake tokens")
	}
}

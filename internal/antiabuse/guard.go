package antiabuse

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"voxhold-backend/internal/httpapi"
)

type clientIPContextKey struct{}

type limiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type limiterStore struct {
	mu         sync.Mutex
	entries    map[string]*limiterEntry
	ttl        time.Duration
	maxEntries int
	operations uint64
}

type failureEntry struct {
	failures     int
	blockedUntil time.Time
	lastSeen     time.Time
}

type Guard struct {
	config   Config
	limiters limiterStore

	failuresMu sync.Mutex
	failures   map[string]failureEntry

	connectionsMu   sync.Mutex
	connectionsIP   map[string]int
	connectionsUser map[int64]int
}

func New(config Config) *Guard {
	return &Guard{
		config: config,
		limiters: limiterStore{
			entries:    make(map[string]*limiterEntry),
			ttl:        config.EntryTTL,
			maxEntries: config.MaxRateEntries,
		},
		failures:        make(map[string]failureEntry),
		connectionsIP:   make(map[string]int),
		connectionsUser: make(map[int64]int),
	}
}

func (g *Guard) ProtectHTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientIP := g.resolveClientIP(r)
		r = r.WithContext(context.WithValue(
			r.Context(),
			clientIPContextKey{},
			clientIP,
		))

		if !g.limiters.allow(
			"http:all:"+clientIP,
			rate.Limit(g.config.HTTPRequestsPerSecond),
			g.config.HTTPBurst,
		) {
			writeRateLimitError(w, time.Second)
			return
		}

		if r.Method != http.MethodGet &&
			r.Method != http.MethodHead &&
			r.Method != http.MethodOptions &&
			!g.limiters.allow(
				"http:write:"+clientIP,
				rate.Limit(g.config.HTTPWritesPerSecond),
				g.config.HTTPWriteBurst,
			) {

			writeRateLimitError(w, time.Second)
			return
		}

		switch {
		case r.Method == http.MethodPost &&
			(r.URL.Path == "/api/v1/auth/login" ||
				r.URL.Path == "/api/v1/auth/register" ||
				r.URL.Path == "/api/v1/auth/refresh"):

			if !g.limiters.allow(
				"http:auth:"+clientIP,
				rate.Limit(float64(g.config.AuthRequestsPerMinute)/60),
				g.config.AuthBurst,
			) {
				writeRateLimitError(w, 6*time.Second)
				return
			}

		case r.Method == http.MethodPost &&
			r.URL.Path == "/api/v1/invite-links/resolve":

			if !g.limiters.allow(
				"http:invite:"+clientIP,
				rate.Limit(float64(g.config.InviteRequestsPerMinute)/60),
				g.config.InviteBurst,
			) {
				writeRateLimitError(w, 3*time.Second)
				return
			}

		case r.Method == http.MethodGet &&
			r.URL.Path == "/api/v1/ws":

			if !g.limiters.allow(
				"http:ws:"+clientIP,
				rate.Limit(float64(g.config.WebSocketConnectsPerMinute)/60),
				g.config.WebSocketConnectBurst,
			) {
				writeRateLimitError(w, 6*time.Second)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

func (g *Guard) AllowLogin(
	r *http.Request,
	username string,
) (bool, time.Duration) {
	now := time.Now()
	keys := g.loginFailureKeys(r, username)

	g.failuresMu.Lock()
	defer g.failuresMu.Unlock()

	g.cleanupFailuresLocked(now)

	for _, key := range keys {
		entry, ok := g.failures[key]
		if !ok || !now.Before(entry.blockedUntil) {
			continue
		}

		return false, time.Until(entry.blockedUntil)
	}

	return true, 0
}

func (g *Guard) RecordLoginFailure(
	r *http.Request,
	username string,
) {
	now := time.Now()

	g.failuresMu.Lock()
	defer g.failuresMu.Unlock()

	g.cleanupFailuresLocked(now)
	for _, key := range g.loginFailureKeys(r, username) {
		entry, exists := g.failures[key]
		if !exists && len(g.failures) >= g.config.MaxRateEntries {
			continue
		}
		entry.failures++
		entry.lastSeen = now
		if entry.failures >= g.config.LoginMaxFailures {
			entry.blockedUntil = now.Add(g.config.LoginBlockDuration)
		}
		g.failures[key] = entry
	}
}

func (g *Guard) RecordLoginSuccess(
	r *http.Request,
	username string,
) {
	g.failuresMu.Lock()
	defer g.failuresMu.Unlock()

	for _, key := range g.loginFailureKeys(r, username) {
		delete(g.failures, key)
	}
}

func (g *Guard) AcquireWebSocketIP(
	r *http.Request,
) (func(), bool) {
	clientIP := ClientIP(r)

	g.connectionsMu.Lock()
	defer g.connectionsMu.Unlock()

	if g.connectionsIP[clientIP] >= g.config.WebSocketMaxPerIP {
		return func() {}, false
	}

	g.connectionsIP[clientIP]++

	return sync.OnceFunc(func() {
		g.connectionsMu.Lock()
		defer g.connectionsMu.Unlock()

		g.connectionsIP[clientIP]--
		if g.connectionsIP[clientIP] == 0 {
			delete(g.connectionsIP, clientIP)
		}
	}), true
}

func (g *Guard) AcquireWebSocketUser(
	userID int64,
) (func(), bool) {
	g.connectionsMu.Lock()
	defer g.connectionsMu.Unlock()

	if g.connectionsUser[userID] >= g.config.WebSocketMaxPerUser {
		return func() {}, false
	}

	g.connectionsUser[userID]++

	return sync.OnceFunc(func() {
		g.connectionsMu.Lock()
		defer g.connectionsMu.Unlock()

		g.connectionsUser[userID]--
		if g.connectionsUser[userID] == 0 {
			delete(g.connectionsUser, userID)
		}
	}), true
}

func (g *Guard) NewWebSocketEventLimiter() func() bool {
	limiter := rate.NewLimiter(
		rate.Limit(g.config.WebSocketEventsPerSecond),
		g.config.WebSocketEventBurst,
	)

	return limiter.Allow
}

func ClientIP(r *http.Request) string {
	if value, ok := r.Context().Value(clientIPContextKey{}).(string); ok &&
		value != "" {

		return value
	}

	return remoteIP(r.RemoteAddr)
}

func (g *Guard) resolveClientIP(r *http.Request) string {
	if g.config.TrustProxyHeaders {
		if value := normalizeIP(r.Header.Get("X-Real-IP")); value != "" {
			return value
		}
	}

	return remoteIP(r.RemoteAddr)
}

func remoteIP(remoteAddress string) string {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err == nil {
		if value := normalizeIP(host); value != "" {
			return value
		}
	}

	if value := normalizeIP(remoteAddress); value != "" {
		return value
	}

	return "unknown"
}

func normalizeIP(value string) string {
	address, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil {
		return ""
	}

	return address.Unmap().String()
}

func (g *Guard) loginFailureKeys(
	r *http.Request,
	username string,
) []string {
	digest := sha256.Sum256([]byte(strings.ToLower(
		strings.TrimSpace(username),
	)))
	identity := hex.EncodeToString(digest[:])
	clientIP := ClientIP(r)

	return []string{
		"login:ip:" + clientIP,
		"login:pair:" + clientIP + ":" + identity,
	}
}

func (g *Guard) cleanupFailuresLocked(now time.Time) {
	if len(g.failures) < g.config.MaxRateEntries {
		return
	}

	for key, entry := range g.failures {
		if now.Sub(entry.lastSeen) >= g.config.EntryTTL &&
			!now.Before(entry.blockedUntil) {

			delete(g.failures, key)
		}
	}
}

func (s *limiterStore) allow(
	key string,
	requestsPerSecond rate.Limit,
	burst int,
) bool {
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	s.operations++
	if s.operations%256 == 0 || len(s.entries) >= s.maxEntries {
		s.cleanupLocked(now)
	}

	entry, ok := s.entries[key]
	if !ok {
		if len(s.entries) >= s.maxEntries {
			return false
		}

		entry = &limiterEntry{
			limiter: rate.NewLimiter(requestsPerSecond, burst),
		}
		s.entries[key] = entry
	}

	entry.lastSeen = now
	return entry.limiter.AllowN(now, 1)
}

func (s *limiterStore) cleanupLocked(now time.Time) {
	for key, entry := range s.entries {
		if now.Sub(entry.lastSeen) >= s.ttl {
			delete(s.entries, key)
		}
	}
}

func writeRateLimitError(
	w http.ResponseWriter,
	retryAfter time.Duration,
) {
	seconds := max(1, int(retryAfter.Round(time.Second)/time.Second))
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	httpapi.WriteError(
		w,
		http.StatusTooManyRequests,
		"too many requests; try again later",
	)
}

package realtimehttp

import (
	"testing"
	"time"
)

func TestTypingLimiterAllowsOncePerInterval(t *testing.T) {
	var limiter typingLimiter
	now := time.Now()
	if !limiter.allow(1, 10, now) {
		t.Fatal("first event refused")
	}
	if limiter.allow(1, 10, now.Add(time.Second)) {
		t.Fatal("second event within 3s allowed")
	}
	if !limiter.allow(1, 11, now.Add(time.Second)) || !limiter.allow(2, 10, now.Add(time.Second)) {
		t.Fatal("other channel or user was throttled")
	}
	if !limiter.allow(1, 10, now.Add(4*time.Second)) {
		t.Fatal("event after the interval refused")
	}
}

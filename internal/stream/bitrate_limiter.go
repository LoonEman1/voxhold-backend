package stream

import (
	"sync"
	"time"
)

type bitrateLimiter struct {
	mu             sync.Mutex
	bytesPerSecond float64
	capacity       float64
	tokens         float64
	updated        time.Time
}

func newBitrateLimiter(kbps int, burstSeconds float64) *bitrateLimiter {
	rate := float64(kbps*1000) / 8
	now := time.Now()
	return &bitrateLimiter{
		bytesPerSecond: rate,
		capacity:       rate * burstSeconds,
		tokens:         rate * burstSeconds,
		updated:        now,
	}
}

func (l *bitrateLimiter) allow(size int) bool {
	if size <= 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.tokens += now.Sub(l.updated).Seconds() * l.bytesPerSecond
	if l.tokens > l.capacity {
		l.tokens = l.capacity
	}
	l.updated = now
	if float64(size) > l.tokens {
		return false
	}
	l.tokens -= float64(size)
	return true
}

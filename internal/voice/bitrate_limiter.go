package voice

import (
	"time"

	"golang.org/x/time/rate"
)

const audioBitrateBurstDuration = 3 * time.Second

type audioBitrateLimiter struct {
	limiter *rate.Limiter
}

func newAudioBitrateLimiter(
	maxBitrateKbps int,
) *audioBitrateLimiter {
	bytesPerSecond := maxBitrateKbps * 1000 / 8
	burstBytes := bytesPerSecond *
		int(audioBitrateBurstDuration/time.Second)

	return &audioBitrateLimiter{
		limiter: rate.NewLimiter(
			rate.Limit(bytesPerSecond),
			burstBytes,
		),
	}
}

func (l *audioBitrateLimiter) allow(
	payloadBytes int,
) bool {
	return l.allowAt(time.Now(), payloadBytes)
}

func (l *audioBitrateLimiter) allowAt(
	now time.Time,
	payloadBytes int,
) bool {
	if payloadBytes <= 0 {
		return true
	}

	return l.limiter.AllowN(now, payloadBytes)
}

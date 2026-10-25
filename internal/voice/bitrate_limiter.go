package voice

import (
	"time"

	"golang.org/x/time/rate"
)

const (
	// The guard only stops abusive clients. Opus VBR legitimately runs above
	// its average target for seconds at a time (laughter, music, noise); a
	// guard set exactly at the configured bitrate disconnected honest speakers
	// from the channel with "audio bitrate limit exceeded".
	audioBitrateHeadroomPercent = 150
	audioBitrateBurstDuration   = 4 * time.Second
)

type audioBitrateLimiter struct {
	limiter *rate.Limiter
}

func newAudioBitrateLimiter(
	maxBitrateKbps int,
) *audioBitrateLimiter {
	bytesPerSecond := maxBitrateKbps * 1000 / 8 *
		audioBitrateHeadroomPercent / 100
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

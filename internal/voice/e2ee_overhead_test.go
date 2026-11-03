package voice

import (
	"testing"
	"time"
)

// FEAT-13 adds up to 19 bytes to every 20 ms Opus frame (+7.6 kbit/s). A
// client at the maximum bitrate must still pass the abuse guard.
func TestAudioLimiterLeavesRoomForE2EEOverhead(t *testing.T) {
	const maxKbps = 128
	limiter := newAudioBitrateLimiter(maxKbps)
	frame := maxKbps*1000/8/50 + 19
	start := time.Now()
	for i := 0; i < 50*30; i++ {
		if !limiter.allowAt(start.Add(time.Duration(i)*20*time.Millisecond), frame) {
			t.Fatalf("encrypted frame %d at the maximum bitrate was rejected", i)
		}
	}
}

package voice

import (
	"testing"
	"time"
)

func TestAudioBitrateLimiterAllowsConfiguredRate(t *testing.T) {
	limiter := newAudioBitrateLimiter(128)
	now := time.Unix(1, 0)
	bytesPerSecond := 128 * 1000 / 8

	for second := 0; second < 10; second++ {
		if !limiter.allowAt(
			now.Add(time.Duration(second)*time.Second),
			bytesPerSecond,
		) {
			t.Fatalf("configured bitrate rejected at second %d", second)
		}
	}
}

func TestAudioBitrateLimiterRejectsSustainedExcess(t *testing.T) {
	limiter := newAudioBitrateLimiter(128)
	now := time.Unix(1, 0)
	burstBytes := 128 * 1000 / 8 *
		int(audioBitrateBurstDuration/time.Second)

	if !limiter.allowAt(now, burstBytes) {
		t.Fatal("initial burst was unexpectedly rejected")
	}
	if limiter.allowAt(now, 1) {
		t.Fatal("traffic above the burst budget was accepted")
	}
	if !limiter.allowAt(now.Add(time.Second), 128*1000/8) {
		t.Fatal("limiter did not replenish at configured bitrate")
	}
}

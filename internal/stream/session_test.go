package stream

import (
	"sync"
	"testing"
	"time"

	"github.com/pion/rtcp"
)

func TestCandidateMatchesCurrentICEGeneration(t *testing.T) {
	current := "current"
	stale := "stale"
	sdp := "v=0\r\na=ice-ufrag:current\r\n"
	if !candidateMatchesRemoteDescription(sdp, &current) {
		t.Fatal("current ICE candidate was rejected")
	}
	if candidateMatchesRemoteDescription(sdp, &stale) {
		t.Fatal("stale ICE candidate was accepted")
	}
	if !candidateMatchesRemoteDescription(sdp, nil) {
		t.Fatal("candidate without generation was rejected")
	}
}

func TestRequestRecoveryRateLimitsKeyframes(t *testing.T) {
	viewer := &session{
		keyframeCooldown: 2 * time.Second,
		room:             newRoom(1, CodecVP8),
	}
	if err := viewer.requestRecovery(RecoveryActionKeyframe); err != nil {
		t.Fatalf("first keyframe request must pass: %v", err)
	}
	first := viewer.lastKeyframeRequest
	if err := viewer.requestRecovery(RecoveryActionKeyframe); err != nil {
		t.Fatalf("rate limited request must be ignored, not fail: %v", err)
	}
	if !viewer.lastKeyframeRequest.Equal(first) {
		t.Fatal("second keyframe request inside cooldown must not re-arm the timer")
	}
}

func TestRequestRecoveryRejectsUnknownAction(t *testing.T) {
	viewer := &session{keyframeCooldown: time.Second, iceCooldown: time.Second}
	if err := viewer.requestRecovery("reload"); err != ErrRecoveryActionInvalid {
		t.Fatalf("unknown action must return invalid error, got %v", err)
	}
}

func TestRequestKeyFrameTargetsSelectedRenditionSSRC(t *testing.T) {
	var packets []rtcp.Packet
	publisher := &session{
		videoSSRC: map[string]uint32{"sdr": 100, "hdr": 200},
		writeRTCP: func(value []rtcp.Packet) error {
			packets = append(packets, value...)
			return nil
		},
	}
	publisher.requestKeyFrame("hdr")
	if len(packets) != 1 {
		t.Fatalf("expected one PLI, got %d", len(packets))
	}
	pli, ok := packets[0].(*rtcp.PictureLossIndication)
	if !ok || pli.MediaSSRC != 200 {
		t.Fatalf("PLI was routed to the wrong rendition: %#v", packets[0])
	}
}

func TestPublisherRenditionsShareOneVideoBitrateCeiling(t *testing.T) {
	publisher := &session{videoBitrate: newBitrateLimiter(8, 1)}
	if !publisher.videoBitrate.allow(600) {
		t.Fatal("first rendition unexpectedly exceeded the shared ceiling")
	}
	if publisher.videoBitrate.allow(500) {
		t.Fatal("second rendition bypassed the shared publisher ceiling")
	}
}

func TestRequestKeyFrameCoalescesBursts(t *testing.T) {
	var mu sync.Mutex
	sent := 0
	publisher := &session{
		videoSSRC:        map[string]uint32{"sdr": 100},
		keyframeInterval: 40 * time.Millisecond,
		writeRTCP: func([]rtcp.Packet) error {
			mu.Lock()
			sent++
			mu.Unlock()
			return nil
		},
	}
	count := func() int {
		mu.Lock()
		defer mu.Unlock()
		return sent
	}

	for range 10 {
		publisher.requestKeyFrame("sdr")
	}
	if got := count(); got != 1 {
		t.Fatalf("a burst must send exactly one immediate PLI, got %d", got)
	}

	deadline := time.Now().Add(time.Second)
	for count() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := count(); got != 2 {
		t.Fatalf("the burst must end with one trailing PLI, got %d", got)
	}
	time.Sleep(80 * time.Millisecond)
	if got := count(); got != 2 {
		t.Fatalf("no further PLI may follow a flushed burst, got %d", got)
	}
}

func TestRequestKeyFrameSkipsRenditionWithoutTrack(t *testing.T) {
	sent := 0
	publisher := &session{
		videoSSRC:        map[string]uint32{},
		keyframeInterval: time.Hour,
		writeRTCP: func([]rtcp.Packet) error {
			sent++
			return nil
		},
	}
	publisher.requestKeyFrame("sdr")
	publisher.videoSSRC["sdr"] = 100
	publisher.requestKeyFrame("sdr")
	if sent != 1 {
		t.Fatalf("a request before the track existed must not consume the interval, got %d PLIs", sent)
	}
}

func TestIngestCeilingToleratesBurstsAboveNominalRate(t *testing.T) {
	if got := ingestCeilingKbps(16000); got != 24000 {
		t.Fatalf("unexpected ingest ceiling: %d", got)
	}
}

package stream

import (
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

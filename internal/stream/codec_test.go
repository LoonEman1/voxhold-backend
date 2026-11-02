package stream

import (
	"errors"
	"strings"
	"testing"

	"github.com/pion/webrtc/v4"
)

func TestAV1RelayAcceptsFirefoxAnswerAndKeepsRTX(t *testing.T) {
	capability := webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeAV1, ClockRate: 90000, SDPFmtpLine: "level-idx=5;profile=0;tier=0"}
	track := newRelayTrack(capability, "video", "av1", legacySDRRendition(CodecAV1))
	context := newTestTrackLocalContext("firefox", 100, av1PrimaryPT, 0, &captureTrackWriter{})
	context.codec.RTPCodecCapability = webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeAV1, ClockRate: 90000}
	if _, err := track.Bind(context); err != nil {
		t.Fatalf("Firefox omits AV1 FMTP for default Main profile: %v", err)
	}
	if err := track.Unbind(context); err != nil {
		t.Fatal(err)
	}
	context.codec.SDPFmtpLine = "profile=1"
	if _, err := track.Bind(context); !errors.Is(err, webrtc.ErrUnsupportedCodec) {
		t.Fatalf("different AV1 profiles must not bind: %v", err)
	}
	manager := &Manager{videoCodecDescriptors: streamVideoCodecDescriptors()}
	preferences := manager.viewerCodecPreferences(capability)
	if len(preferences) != 2 || preferences[0].PayloadType != av1PrimaryPT || preferences[0].SDPFmtpLine != capability.SDPFmtpLine || preferences[1].PayloadType != webrtc.PayloadType(av1RTXPT) {
		t.Fatalf("Chromium AV1 FMTP must retain the registered primary + RTX pair: %+v", preferences)
	}
}

func TestRelayCodecCompatibilityKeepsProfileBoundaries(t *testing.T) {
	for _, test := range []struct {
		name, mime, first, second string
		matches                   bool
	}{
		{"av1-default", webrtc.MimeTypeAV1, "", "level-idx=5;profile=0;tier=0", true},
		{"av1-order", webrtc.MimeTypeAV1, "tier=0; PROFILE = 0 ; level-idx=5", "profile=0;level-idx=7", true},
		{"av1-high", webrtc.MimeTypeAV1, "profile=1", "profile=0", false},
		{"opus-preferences", webrtc.MimeTypeOpus, "minptime=10;useinbandfec=1;stereo=1;sprop-stereo=1;maxaveragebitrate=128000", "maxplaybackrate=48000;stereo=1;useinbandfec=1", true},
		{"h264-profile", webrtc.MimeTypeH264, "profile-level-id=42e01f", "profile-level-id=42001f", false},
		{"vp9-profile", webrtc.MimeTypeVP9, "profile-id=2", "profile-id=0", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := webrtc.RTPCodecCapability{MimeType: test.mime, ClockRate: 90000, SDPFmtpLine: test.first}
			b := webrtc.RTPCodecCapability{MimeType: test.mime, ClockRate: 90000, SDPFmtpLine: test.second}
			if got := relayCodecCompatible(a, b); got != test.matches {
				t.Fatalf("match = %v, want %v", got, test.matches)
			}
			b.ClockRate = 48000
			if relayCodecCompatible(a, b) {
				t.Fatal("clock rate mismatch accepted")
			}
		})
	}
}

func TestStreamVideoCodecsHaveUniquePayloadTypes(t *testing.T) {
	descriptors := streamVideoCodecDescriptors()
	seen := make(map[webrtc.PayloadType]string)
	for _, descriptor := range descriptors {
		for _, pair := range []struct {
			pt    webrtc.PayloadType
			label string
		}{
			{descriptor.primary.PayloadType, "primary " + descriptor.primary.MimeType},
			{webrtc.PayloadType(descriptor.rtxPT), "rtx for " + descriptor.primary.MimeType},
		} {
			if owner, exists := seen[pair.pt]; exists {
				t.Fatalf("payload type %d reused by %s and %s", pair.pt, owner, pair.label)
			}
			seen[pair.pt] = pair.label
		}
	}
}

func TestStreamVideoCodecsRegisterRTXPairs(t *testing.T) {
	for _, descriptor := range streamVideoCodecDescriptors() {
		if descriptor.rtxPT == 0 {
			t.Fatalf("%s has no RTX payload type", descriptor.primary.MimeType)
		}
		rtx, ok := rtxForDescriptor(descriptor)
		if !ok {
			t.Fatalf("%s RTX entry missing", descriptor.primary.MimeType)
		}
		if rtx.MimeType != "video/rtx" {
			t.Fatalf("expected video/rtx, got %q", rtx.MimeType)
		}
		wantAPT := "apt=" + itoa(int(descriptor.primary.PayloadType))
		if rtx.SDPFmtpLine != wantAPT {
			t.Fatalf(
				"RTX apt mismatch for %s: got %q want %q",
				descriptor.primary.MimeType,
				rtx.SDPFmtpLine,
				wantAPT,
			)
		}
	}
}

func TestH264ConstrainedBaselineComesFirst(t *testing.T) {
	descriptors := streamVideoCodecDescriptors()
	firstH264Index := -1
	for index, descriptor := range descriptors {
		if descriptor.primary.MimeType == webrtc.MimeTypeH264 {
			firstH264Index = index
			break
		}
	}
	if firstH264Index < 0 {
		t.Fatal("no H.264 codec registered")
	}
	first := descriptors[firstH264Index].primary.SDPFmtpLine
	if !strings.Contains(first, "42e01f") {
		t.Fatalf("first H.264 payload must be Constrained Baseline 42e01f, got %q", first)
	}
	legacySeen := false
	for _, descriptor := range descriptors[firstH264Index+1:] {
		if descriptor.primary.MimeType != webrtc.MimeTypeH264 {
			continue
		}
		if strings.Contains(descriptor.primary.SDPFmtpLine, "42001f") {
			legacySeen = true
		} else if strings.Contains(descriptor.primary.SDPFmtpLine, "42e01f") {
			t.Fatal("secondary Constrained Baseline must not precede the legacy profile")
		}
	}
	if !legacySeen {
		t.Fatal("legacy 42001f H.264 profile must stay registered for old clients")
	}
}

func TestPrimaryCodecsKeepLossFeedback(t *testing.T) {
	for _, descriptor := range streamVideoCodecDescriptors() {
		feedback := descriptor.primary.RTCPFeedback
		hasNACK, hasPLI, hasREMB := false, false, false
		for _, item := range feedback {
			switch {
			case item.Type == "nack" && item.Parameter == "":
				hasNACK = true
			case item.Type == "nack" && item.Parameter == "pli":
				hasPLI = true
			case item.Type == "goog-remb":
				hasREMB = true
			}
		}
		if !hasNACK || !hasPLI || !hasREMB {
			t.Fatalf(
				"%s primary is missing loss feedback: nack=%v pli=%v remb=%v",
				descriptor.primary.MimeType,
				hasNACK,
				hasPLI,
				hasREMB,
			)
		}
	}
}

func TestStreamVideoCodecsNeverReturnRTXAsPrimary(t *testing.T) {
	for _, codec := range streamVideoCodecs() {
		if strings.EqualFold(codec.MimeType, "video/rtx") {
			t.Fatal("video/rtx must never appear as a primary codec")
		}
	}
}

func TestViewerCodecPreferencesMatchesExactCapability(t *testing.T) {
	manager := &Manager{videoCodecDescriptors: streamVideoCodecDescriptors()}
	constrained := manager.viewerCodecPreferences(webrtc.RTPCodecCapability{
		MimeType:    webrtc.MimeTypeH264,
		ClockRate:   90000,
		SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f",
	})
	if len(constrained) != 2 {
		t.Fatalf("expected primary + rtx, got %d entries", len(constrained))
	}
	if constrained[0].PayloadType != h264CBPrimary {
		t.Fatalf("expected constrained baseline PT %d, got %d", h264CBPrimary, constrained[0].PayloadType)
	}
	if constrained[1].PayloadType != webrtc.PayloadType(h264CBRTX) {
		t.Fatalf("expected RTX PT %d, got %d", h264CBRTX, constrained[1].PayloadType)
	}

	legacy := manager.viewerCodecPreferences(webrtc.RTPCodecCapability{
		MimeType:    webrtc.MimeTypeH264,
		ClockRate:   90000,
		SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42001f",
	})
	if legacy[0].PayloadType != h264LegPrimary {
		t.Fatalf("legacy capability must map to its own PT, got %d", legacy[0].PayloadType)
	}

	unknown := manager.viewerCodecPreferences(webrtc.RTPCodecCapability{
		MimeType:    webrtc.MimeTypeVP8,
		ClockRate:   90000,
		SDPFmtpLine: "unknown-profile",
	})
	if len(unknown) != 1 || unknown[0].RTPCodecCapability.SDPFmtpLine != "unknown-profile" {
		t.Fatalf("unknown capability must pass through untouched, got %+v", unknown)
	}
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	negative := value < 0
	if negative {
		value = -value
	}
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	if negative {
		return "-" + digits
	}
	return digits
}

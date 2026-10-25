package stream

import (
	"strings"
	"testing"
)

func TestWithVideoBandwidthCapsOnlyVideoSections(t *testing.T) {
	sdp := strings.Join([]string{
		"v=0",
		"o=- 1 1 IN IP4 0.0.0.0",
		"s=-",
		"t=0 0",
		"m=video 9 UDP/TLS/RTP/SAVPF 96",
		"c=IN IP4 0.0.0.0",
		"b=AS:30000",
		"a=mid:0",
		"m=audio 9 UDP/TLS/RTP/SAVPF 111",
		"c=IN IP4 0.0.0.0",
		"a=mid:1",
		"m=video 9 UDP/TLS/RTP/SAVPF 98",
		"c=IN IP4 0.0.0.0",
		"a=mid:2",
		"",
	}, "\r\n")

	got := withVideoBandwidth(sdp, 8000)

	if strings.Contains(got, "b=AS:30000") {
		t.Fatalf("existing video bandwidth line was not replaced:\n%s", got)
	}
	if count := strings.Count(got, "b=AS:8000\r\n"); count != 2 {
		t.Fatalf("expected b=AS in both video sections, got %d:\n%s", count, got)
	}
	audio := got[strings.Index(got, "m=audio"):strings.LastIndex(got, "m=video")]
	if strings.Contains(audio, "b=AS") {
		t.Fatalf("audio section must stay untouched:\n%s", audio)
	}
	if !strings.Contains(got, "c=IN IP4 0.0.0.0\r\nb=AS:8000\r\na=mid:2\r\n") {
		t.Fatalf("b=AS must follow the connection line:\n%s", got)
	}
	if !strings.HasSuffix(got, "\r\n") {
		t.Fatal("trailing CRLF was lost")
	}
}

func TestWithVideoBandwidthHandlesSectionWithoutAttributes(t *testing.T) {
	got := withVideoBandwidth("v=0\nm=video 9 RTP/AVP 96\nc=IN IP4 0.0.0.0\n", 2500)
	if got != "v=0\nm=video 9 RTP/AVP 96\nc=IN IP4 0.0.0.0\nb=AS:2500\n" {
		t.Fatalf("unexpected SDP: %q", got)
	}
}

func TestWithVideoBandwidthIgnoresInvalidLimit(t *testing.T) {
	const sdp = "v=0\r\nm=video 9 RTP/AVP 96\r\n"
	if got := withVideoBandwidth(sdp, 0); got != sdp {
		t.Fatalf("zero limit must leave SDP untouched, got %q", got)
	}
}

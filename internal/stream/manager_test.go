package stream

import (
	"net"
	"strings"
	"sync"
	"testing"
)

type testSink struct {
	mu     sync.Mutex
	offers map[string]string
}

func (s *testSink) SendOffer(connectionID string, sdp string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.offers == nil {
		s.offers = make(map[string]string)
	}
	s.offers[connectionID] = sdp
	return true
}
func (*testSink) SendICECandidate(string, ICECandidate) bool { return true }
func (*testSink) CloseStream(string, string)                 {}

func TestManagerCreatesPublisherAndViewerOffers(t *testing.T) {
	connection, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		t.Fatal(err)
	}
	port := connection.LocalAddr().(*net.UDPAddr).Port
	_ = connection.Close()

	sink := &testSink{}
	manager, err := NewManager(
		Config{
			UDPPort:             port,
			MaxViewers:          4,
			MaxVideoBitrateKbps: 12000,
			MaxAudioBitrateKbps: 320,
		},
		sink,
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close() })

	tests := []struct {
		codec  Codec
		rtpmap string
	}{
		{codec: CodecVP8, rtpmap: "VP8/90000"},
		{codec: CodecVP9, rtpmap: "VP9/90000"},
		{codec: CodecH264, rtpmap: "H264/90000"},
		{codec: CodecAV1, rtpmap: "AV1/90000"},
	}

	for index, test := range tests {
		publisherID := "publisher-" + string(rune('0'+index))
		viewerID := "viewer-" + string(rune('0'+index))
		channelID := int64(100 + index)

		if err := manager.Start(
			publisherID, int64(index+1), 10, channelID, test.codec, true,
		); err != nil {
			t.Fatalf("start %s publisher: %v", test.codec, err)
		}
		if err := manager.Watch(
			viewerID, int64(index+10), 10, channelID,
		); err != nil {
			t.Fatalf("start %s viewer before tracks arrive: %v", test.codec, err)
		}

		sink.mu.Lock()
		publisherOffer := sink.offers[publisherID]
		viewerOffer := sink.offers[viewerID]
		sink.mu.Unlock()

		if publisherOffer == "" {
			t.Fatalf("missing %s publisher WebRTC offer", test.codec)
		}
		if !strings.Contains(publisherOffer, "m=video") ||
			!strings.Contains(publisherOffer, "a=ice-ufrag:") ||
			!strings.Contains(publisherOffer, test.rtpmap) {

			t.Fatalf("%s publisher offer has no selected codec: %q", test.codec, publisherOffer)
		}
		for _, other := range tests {
			if other.codec != test.codec && strings.Contains(publisherOffer, other.rtpmap) {
				t.Fatalf("%s publisher offer unexpectedly contains %s", test.codec, other.codec)
			}
		}
		for _, feedback := range []string{
			" nack\r\n",
			" nack pli\r\n",
			" transport-cc\r\n",
			"transport-wide-cc-extensions",
		} {
			if !strings.Contains(publisherOffer, feedback) {
				t.Fatalf(
					"%s publisher offer is missing WebRTC loss feedback %q",
					test.codec,
					feedback,
				)
			}
		}
		if viewerOffer != "" {
			t.Fatalf("%s viewer received an empty-track offer", test.codec)
		}
	}
}

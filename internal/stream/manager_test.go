package stream

import (
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"

	"voxhold-backend/internal/webrtcrecovery"
)

type testSink struct {
	mu     sync.Mutex
	offers map[string]string
}

func TestStreamSessionRestartsICEAfterFailure(t *testing.T) {
	connection, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		t.Fatal(err)
	}
	port := connection.LocalAddr().(*net.UDPAddr).Port
	_ = connection.Close()

	sink := &testSink{}
	manager, err := NewManager(Config{
		UDPPort:             port,
		MaxViewers:          4,
		MaxVideoBitrateKbps: 12000,
		MaxAudioBitrateKbps: 320,
	}, sink)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close() })

	const connectionID = "recovering-publisher"
	if err := manager.Start(
		connectionID, 1, 10, 100, CodecVP9, false,
	); err != nil {
		t.Fatal(err)
	}

	peer, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = peer.Close() })

	initialOffer := sink.offer(connectionID)
	acceptTestOffer(t, manager, peer, connectionID, initialOffer)
	initialUfrag := iceUfrag(initialOffer)
	if initialUfrag == "" {
		t.Fatal("initial offer has no ICE ufrag")
	}

	value := manager.session(connectionID)
	value.recovery.Stop()
	value.recovery = webrtcrecovery.New(webrtcrecovery.Policy{
		RetryDelay:  time.Second,
		MaxAttempts: 2,
	})
	value.handleConnectionState(webrtc.PeerConnectionStateFailed)

	var restartedOffer string
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		restartedOffer = sink.offer(connectionID)
		if iceUfrag(restartedOffer) != initialUfrag {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if restartedUfrag := iceUfrag(restartedOffer); restartedUfrag == "" ||
		restartedUfrag == initialUfrag {

		t.Fatalf(
			"ICE restart did not change credentials: before=%q after=%q",
			initialUfrag,
			restartedUfrag,
		)
	}
	acceptTestOffer(t, manager, peer, connectionID, restartedOffer)
	value.handleConnectionState(webrtc.PeerConnectionStateConnected)
	if manager.session(connectionID) != value {
		t.Fatal("successful ICE restart replaced or removed the stream session")
	}
}

func acceptTestOffer(
	t *testing.T,
	manager *Manager,
	peer *webrtc.PeerConnection,
	connectionID string,
	sdp string,
) {
	t.Helper()
	if err := peer.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  sdp,
	}); err != nil {
		t.Fatalf("set remote offer: %v", err)
	}
	answer, err := peer.CreateAnswer(nil)
	if err != nil {
		t.Fatalf("create answer: %v", err)
	}
	if err := peer.SetLocalDescription(answer); err != nil {
		t.Fatalf("set local answer: %v", err)
	}
	if err := manager.AcceptAnswer(connectionID, answer.SDP); err != nil {
		t.Fatalf("accept answer: %v", err)
	}
}

func iceUfrag(sdp string) string {
	const prefix = "a=ice-ufrag:"
	start := strings.Index(sdp, prefix)
	if start < 0 {
		return ""
	}
	value := sdp[start+len(prefix):]
	if end := strings.IndexAny(value, "\r\n"); end >= 0 {
		value = value[:end]
	}
	return value
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
func (s *testSink) offer(connectionID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.offers[connectionID]
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

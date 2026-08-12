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

	if err := manager.Start("publisher", 1, 10, 100, true); err != nil {
		t.Fatalf("start publisher: %v", err)
	}
	if err := manager.Watch("viewer", 2, 10, 100); err != nil {
		t.Fatalf("start viewer before tracks arrive: %v", err)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	publisherOffer := sink.offers["publisher"]
	if publisherOffer == "" {
		t.Fatalf("missing publisher WebRTC offer: %+v", sink.offers)
	}
	if !strings.Contains(publisherOffer, "m=video") ||
		!strings.Contains(publisherOffer, "a=ice-ufrag:") {

		t.Fatalf("publisher offer has no usable media/ICE section: %q", publisherOffer)
	}
	if sink.offers["viewer"] != "" {
		t.Fatal("viewer must not receive an empty offer before publisher tracks arrive")
	}
}

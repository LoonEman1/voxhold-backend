package voice

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

func TestManagerRelaysOpusBetweenPeers(t *testing.T) {
	sink := newTestSignalSink()
	manager := newTestManager(t, sink)
	sink.manager = manager

	first := newTestPeer(t, manager, "first")
	second := newTestPeer(t, manager, "second")
	sink.addPeer(first)
	sink.addPeer(second)

	if err := manager.Join(
		first.connectionID,
		1,
		10,
		100,
		false,
		false,
	); err != nil {
		t.Fatalf("join first peer: %v", err)
	}
	if offer := sink.offer(first.connectionID); !strings.Contains(offer, "maxaveragebitrate=128000") {

		t.Fatalf("audio bitrate limit is missing from SDP: %q", offer)
	}

	first.startAudio(t)
	waitForPublishedTrack(t, manager, 100)

	if err := manager.Join(
		second.connectionID,
		2,
		10,
		100,
		false,
		false,
	); err != nil {
		t.Fatalf("join second peer: %v", err)
	}

	select {
	case packet := <-second.incomingRTP:
		if packet.PayloadType != 111 || len(packet.Payload) == 0 {
			t.Fatalf("unexpected relayed RTP packet: %+v", packet)
		}

	case <-time.After(10 * time.Second):
		t.Fatal("second peer did not receive relayed Opus RTP")
	}

	manager.Leave(first.connectionID)
	if err := manager.AcceptAnswer(
		first.connectionID,
		"unused",
	); !errors.Is(err, ErrSessionNotFound) {

		t.Fatalf("left session still accepts answers: %v", err)
	}
}

func TestManagerRestartsVoiceICEWithoutReplacingSession(t *testing.T) {
	sink := newTestSignalSink()
	manager := newTestManager(t, sink)
	sink.manager = manager

	peer := newTestPeer(t, manager, "recovering-voice")
	sink.addPeer(peer)
	if err := manager.Join(
		peer.connectionID, 1, 10, 100, false, false,
	); err != nil {
		t.Fatal(err)
	}

	initialUfrag := testICEUfrag(sink.offer(peer.connectionID))
	value := manager.session(peer.connectionID)
	if value == nil {
		t.Fatal("voice session is missing")
	}
	if err := value.restartICE(); err != nil {
		t.Fatalf("restart voice ICE: %v", err)
	}
	restartedUfrag := testICEUfrag(sink.offer(peer.connectionID))
	if initialUfrag == "" || restartedUfrag == "" ||
		initialUfrag == restartedUfrag {

		t.Fatalf(
			"ICE restart did not change credentials: before=%q after=%q",
			initialUfrag,
			restartedUfrag,
		)
	}
	if manager.session(peer.connectionID) != value {
		t.Fatal("ICE restart replaced or removed the voice session")
	}
	if err := manager.SetState(peer.connectionID, true, false); err != nil {
		t.Fatalf("recovered voice session cannot update state: %v", err)
	}
}

func testICEUfrag(sdp string) string {
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

func TestManagerClosesPeerThatExceedsAudioBitrate(t *testing.T) {
	sink := newTestSignalSink()
	manager := newTestManagerWithAudioBitrate(t, sink, 16)
	sink.manager = manager

	peer := newTestPeer(t, manager, "excessive-sender")
	sink.addPeer(peer)

	if err := manager.Join(
		peer.connectionID,
		1,
		10,
		100,
		false,
		false,
	); err != nil {
		t.Fatalf("join peer: %v", err)
	}

	peer.startAudio(t)
	waitForPublishedTrack(t, manager, 100)
	peer.sendAudioBurst(t, 64, 1200)

	select {
	case closed := <-sink.closed:
		if closed.connectionID != peer.connectionID ||
			closed.reason != "audio bitrate limit exceeded" {

			t.Fatalf("unexpected closed session: %+v", closed)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("excessive audio bitrate did not close the session")
	}

	if err := manager.SetState(
		peer.connectionID,
		false,
		false,
	); !errors.Is(err, ErrSessionNotFound) {

		t.Fatalf("limited session is still active: %v", err)
	}
}

type testClosedSession struct {
	connectionID string
	reason       string
}

func TestManagerExcludesLoopbackMuxWhenPublicIPIsConfigured(
	t *testing.T,
) {
	sink := newTestSignalSink()

	for attempt := 0; attempt < 10; attempt++ {
		manager, err := NewManager(
			Config{
				UDPPort:         unusedUDPPort(t),
				MaxParticipants: DefaultMaxParticipants,
				PublicIP:        "127.0.0.1",
			},
			sink,
		)
		if err != nil {
			continue
		}
		defer func() {
			_ = manager.Close()
		}()

		for _, address := range manager.udpMux.GetListenAddresses() {
			udpAddress, ok := address.(*net.UDPAddr)
			if ok && udpAddress.IP.IsLoopback() {
				t.Fatalf(
					"public IP manager listens through loopback mux: %s",
					address,
				)
			}
		}

		return
	}

	t.Fatal("could not allocate WebRTC UDP port")
}

type testSignalSink struct {
	mu      sync.RWMutex
	manager *Manager
	peers   map[string]*testPeer
	offers  map[string]string
	closed  chan testClosedSession
}

func newTestSignalSink() *testSignalSink {
	return &testSignalSink{
		peers:  make(map[string]*testPeer),
		offers: make(map[string]string),
		closed: make(chan testClosedSession, 16),
	}
}

func (s *testSignalSink) addPeer(peer *testPeer) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.peers[peer.connectionID] = peer
}

func (s *testSignalSink) SendOffer(
	connectionID string,
	sdp string,
) bool {
	s.mu.Lock()
	peer := s.peers[connectionID]
	s.offers[connectionID] = sdp
	s.mu.Unlock()

	if peer == nil ||
		!strings.Contains(sdp, "m=audio") ||
		strings.Contains(sdp, "m=video") {

		return false
	}

	return peer.acceptOffer(sdp) == nil
}

func (s *testSignalSink) offer(connectionID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.offers[connectionID]
}

func (s *testSignalSink) SendICECandidate(
	connectionID string,
	candidate ICECandidate,
) bool {
	peer := s.peer(connectionID)
	return peer != nil && peer.addICECandidate(candidate) == nil
}

func (s *testSignalSink) CloseVoice(
	connectionID string,
	reason string,
) {
	select {
	case s.closed <- testClosedSession{
		connectionID: connectionID,
		reason:       reason,
	}:
	default:
	}
}

func (s *testSignalSink) peer(
	connectionID string,
) *testPeer {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.peers[connectionID]
}

type testPeer struct {
	connectionID string
	manager      *Manager
	peer         *webrtc.PeerConnection
	localTrack   *webrtc.TrackLocalStaticRTP
	incomingRTP  chan *rtp.Packet

	mu                sync.Mutex
	pendingCandidates []ICECandidate
	stopAudio         chan struct{}
	closeOnce         sync.Once
}

func newTestPeer(
	t *testing.T,
	manager *Manager,
	connectionID string,
) *testPeer {
	t.Helper()

	peerConnection, err := webrtc.NewPeerConnection(
		webrtc.Configuration{},
	)
	if err != nil {
		t.Fatalf("create test peer connection: %v", err)
	}

	localTrack, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeOpus,
			ClockRate:   48000,
			Channels:    2,
			SDPFmtpLine: "minptime=10;useinbandfec=1",
		},
		"microphone-"+connectionID,
		"test-stream-"+connectionID,
	)
	if err != nil {
		t.Fatalf("create test Opus track: %v", err)
	}

	sender, err := peerConnection.AddTrack(localTrack)
	if err != nil {
		t.Fatalf("add test Opus track: %v", err)
	}
	go drainRTCP(sender)

	value := &testPeer{
		connectionID: connectionID,
		manager:      manager,
		peer:         peerConnection,
		localTrack:   localTrack,
		incomingRTP:  make(chan *rtp.Packet, 32),
		stopAudio:    make(chan struct{}),
	}

	peerConnection.OnICECandidate(
		func(candidate *webrtc.ICECandidate) {
			if candidate == nil {
				return
			}

			jsonCandidate := candidate.ToJSON()
			_ = manager.AddICECandidate(
				connectionID,
				ICECandidate{
					Candidate:        jsonCandidate.Candidate,
					SDPMid:           jsonCandidate.SDPMid,
					SDPMLineIndex:    jsonCandidate.SDPMLineIndex,
					UsernameFragment: jsonCandidate.UsernameFragment,
				},
			)
		},
	)

	peerConnection.OnTrack(
		func(
			track *webrtc.TrackRemote,
			_ *webrtc.RTPReceiver,
		) {
			for {
				packet, _, err := track.ReadRTP()
				if err != nil {
					return
				}

				select {
				case value.incomingRTP <- packet:
				default:
				}
			}
		},
	)

	t.Cleanup(func() {
		value.close()
	})

	return value
}

func (p *testPeer) acceptOffer(sdp string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.peer.SetRemoteDescription(
		webrtc.SessionDescription{
			Type: webrtc.SDPTypeOffer,
			SDP:  sdp,
		},
	); err != nil {
		return fmt.Errorf("set test remote offer: %w", err)
	}

	for _, candidate := range p.pendingCandidates {
		if err := p.addICECandidateLocked(candidate); err != nil {
			return err
		}
	}
	p.pendingCandidates = nil

	answer, err := p.peer.CreateAnswer(nil)
	if err != nil {
		return fmt.Errorf("create test answer: %w", err)
	}
	if err := p.peer.SetLocalDescription(answer); err != nil {
		return fmt.Errorf("set test local answer: %w", err)
	}

	if err := p.manager.AcceptAnswer(
		p.connectionID,
		answer.SDP,
	); err != nil {
		return err
	}

	return nil
}

func (p *testPeer) addICECandidate(
	candidate ICECandidate,
) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.peer.RemoteDescription() == nil {
		p.pendingCandidates = append(
			p.pendingCandidates,
			candidate,
		)
		return nil
	}

	return p.addICECandidateLocked(candidate)
}

func (p *testPeer) addICECandidateLocked(
	candidate ICECandidate,
) error {
	return p.peer.AddICECandidate(
		webrtc.ICECandidateInit{
			Candidate:        candidate.Candidate,
			SDPMid:           candidate.SDPMid,
			SDPMLineIndex:    candidate.SDPMLineIndex,
			UsernameFragment: candidate.UsernameFragment,
		},
	)
}

func (p *testPeer) startAudio(t *testing.T) {
	t.Helper()

	go func() {
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()

		var sequenceNumber uint16
		var timestamp uint32

		for {
			select {
			case <-p.stopAudio:
				return

			case <-ticker.C:
				packet := &rtp.Packet{
					Header: rtp.Header{
						Version:        2,
						PayloadType:    111,
						SequenceNumber: sequenceNumber,
						Timestamp:      timestamp,
						SSRC:           12345,
					},
					Payload: []byte{0xf8, 0xff, 0xfe},
				}

				_ = p.localTrack.WriteRTP(packet)
				sequenceNumber++
				timestamp += 960
			}
		}
	}()
}

func (p *testPeer) sendAudioBurst(
	t *testing.T,
	packetCount int,
	payloadSize int,
) {
	t.Helper()

	payload := make([]byte, payloadSize)
	for index := 0; index < packetCount; index++ {
		packet := &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				PayloadType:    111,
				SequenceNumber: uint16(1000 + index),
				Timestamp:      uint32(960 * (1000 + index)),
				SSRC:           12345,
			},
			Payload: payload,
		}

		if err := p.localTrack.WriteRTP(packet); err != nil {
			t.Fatalf("write audio burst packet %d: %v", index, err)
		}
	}
}

func (p *testPeer) close() {
	p.closeOnce.Do(func() {
		close(p.stopAudio)
		_ = p.peer.Close()
	})
}

func newTestManager(
	t *testing.T,
	sink SignalSink,
) *Manager {
	return newTestManagerWithAudioBitrate(
		t,
		sink,
		DefaultMaxAudioBitrateKbps,
	)
}

func newTestManagerWithAudioBitrate(
	t *testing.T,
	sink SignalSink,
	maxAudioBitrateKbps int,
) *Manager {
	t.Helper()

	for attempt := 0; attempt < 10; attempt++ {
		port := unusedUDPPort(t)
		manager, err := NewManager(
			Config{
				UDPPort:             port,
				MaxParticipants:     DefaultMaxParticipants,
				MaxAudioBitrateKbps: maxAudioBitrateKbps,
			},
			sink,
		)
		if err == nil {
			t.Cleanup(func() {
				_ = manager.Close()
			})
			return manager
		}
	}

	t.Fatal("could not allocate a WebRTC UDP port")
	return nil
}

func unusedUDPPort(t *testing.T) int {
	t.Helper()

	connection, err := net.ListenUDP(
		"udp4",
		&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)},
	)
	if err != nil {
		t.Fatalf("allocate test UDP port: %v", err)
	}

	port := connection.LocalAddr().(*net.UDPAddr).Port
	_ = connection.Close()

	return port
}

func waitForPublishedTrack(
	t *testing.T,
	manager *Manager,
	channelID int64,
) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		manager.mu.RLock()
		voiceRoom := manager.rooms[channelID]
		manager.mu.RUnlock()

		if voiceRoom != nil && len(voiceRoom.trackSnapshot()) > 0 {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("server did not receive the first Opus track")
}

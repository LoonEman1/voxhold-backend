package stream

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// Real ICE/DTLS/SRTP connections: distinguish the publishers by packet payload,
// not only by the manager's in-memory room pointers.
func TestTwoPublishersRelayDistinctRTPAndOneSurvivesTheOtherStopping(t *testing.T) {
	sink := &testSink{}
	manager, err := NewManager(Config{UDPPort: 0}, sink)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	type source struct {
		stop    func()
		sent    *atomic.Uint32
		packets chan *rtp.Packet
	}
	sources := make([]source, 2)
	for i, id := range []string{"first", "second"} {
		publisher, err := webrtc.NewPeerConnection(webrtc.Configuration{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = publisher.Close() })
		track, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000}, "screen", id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := publisher.AddTrack(track); err != nil {
			t.Fatal(err)
		}
		if err := manager.Start(id, int64(i+1), 10, 100, CodecVP8, false); err != nil {
			t.Fatal(err)
		}
		acceptTestOffer(t, manager, publisher, id, sink.offer(id))
		awaitStreamCondition(t, func() bool { return publisher.ConnectionState() == webrtc.PeerConnectionStateConnected })
		done, stopped := make(chan struct{}), make(chan struct{})
		var once sync.Once
		sequence := &atomic.Uint32{}
		stop := func() { once.Do(func() { close(done) }); <-stopped }
		t.Cleanup(stop)
		go func(tag byte) {
			defer close(stopped)
			ticker := time.NewTicker(20 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-done:
					return
				case <-ticker.C:
					n := sequence.Add(1)
					_ = track.WriteRTP(&rtp.Packet{Header: rtp.Header{Version: 2, Marker: true, SequenceNumber: uint16(n), Timestamp: n * 1800}, Payload: []byte{0x10, 0, tag}})
				}
			}
		}(byte(i + 1))
		awaitStreamCondition(t, func() bool { return len(manager.session(id).room.trackSnapshot("sdr")) > 0 })
		viewer, err := webrtc.NewPeerConnection(webrtc.Configuration{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = viewer.Close() })
		packets := make(chan *rtp.Packet, 128)
		viewer.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
			for {
				packet, _, err := remote.ReadRTP()
				if err != nil {
					return
				}
				select {
				case packets <- packet:
				default:
				}
			}
		})
		viewerID := "viewer-" + id
		if err := manager.WatchRenditionForPublisher(viewerID, int64(i+10), 10, 100, id, "sdr"); err != nil {
			t.Fatal(err)
		}
		acceptTestOffer(t, manager, viewer, viewerID, sink.offer(viewerID))
		sources[i] = source{stop, sequence, packets}
	}
	read := func(index int, after uint32) {
		t.Helper()
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		for count := 0; count < 5; {
			select {
			case packet := <-sources[index].packets:
				if len(packet.Payload) == 0 || packet.Payload[len(packet.Payload)-1] != byte(index+1) {
					t.Fatal("RTP crossed publisher rooms")
				}
				if uint32(packet.SequenceNumber) > after {
					count++
				}
			case <-timer.C:
				t.Fatal("selected publisher stopped delivering RTP")
			}
		}
	}
	read(0, 0)
	read(1, 0)
	second := manager.session("second")
	before := sources[1].sent.Load()
	sources[0].stop()
	manager.Leave("first")
	read(1, before)
	if manager.session("second") != second || manager.session("viewer-second") == nil {
		t.Fatal("another stream was replaced during stop")
	}
}

func awaitStreamCondition(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("stream condition timed out")
}

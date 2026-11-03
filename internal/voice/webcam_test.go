package voice

import (
	"testing"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

func TestCameraAndMicrophoneRelayIndependently(t *testing.T) {
	sink := newTestSignalSink()
	manager := newTestManager(t, sink)
	sink.manager = manager
	publisher := newTestPeer(t, manager, "camera-publisher")
	viewer := newTestPeer(t, manager, "camera-viewer")
	sink.addPeer(publisher)
	sink.addPeer(viewer)
	camera, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000}, "webcam", "camera")
	if err != nil {
		t.Fatal(err)
	}
	sender, err := publisher.peer.AddTrack(camera)
	if err != nil {
		t.Fatal(err)
	}
	go drainRTCP(sender)
	if err := manager.Join(publisher.connectionID, 1, 10, 100, false, false); err != nil {
		t.Fatal(err)
	}
	if err := manager.Join(viewer.connectionID, 2, 10, 100, false, false); err != nil {
		t.Fatal(err)
	}
	publisher.startAudio(t)
	stop, stopped := make(chan struct{}), make(chan struct{})
	defer func() { close(stop); <-stopped }()
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		var sequence uint16
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				_ = camera.WriteRTP(&rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: 96, SequenceNumber: sequence, Timestamp: uint32(sequence) * 1800, SSRC: 54321}, Payload: []byte{0x10, 0, 0, 0}})
				sequence++
			}
		}
	}()
	waitForRoomTrackCount(t, manager, 100, 2)
	await := func(wantAudio bool) {
		t.Helper()
		gotAudio, gotVideo := false, false
		deadline := time.After(10 * time.Second)
		for !gotVideo || wantAudio && !gotAudio {
			select {
			case packet := <-viewer.incomingRTP:
				if packet.PayloadType == 111 {
					gotAudio = true
				} else if packet.PayloadType == 96 {
					gotVideo = true
				}
			case <-deadline:
				t.Fatalf("missing independent media: audio=%v video=%v", gotAudio, gotVideo)
			}
		}
	}
	await(true)
	if err := manager.SetState(publisher.connectionID, true, false); err != nil {
		t.Fatal(err)
	}
	if err := manager.SetState(viewer.connectionID, false, true); err != nil {
		t.Fatal(err)
	}
	await(false)
	manager.Leave(publisher.connectionID)
	waitForRoomTrackCount(t, manager, 100, 0)
}

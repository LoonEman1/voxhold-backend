package realtimehttp

import (
	"encoding/json"
	"fmt"
	"github.com/pion/webrtc/v4"
	"testing"
)

func TestMaximumVoiceRoomAnswerFitsWebSocketLimit(t *testing.T) {
	media := &webrtc.MediaEngine{}
	codec := webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2, SDPFmtpLine: "minptime=10;useinbandfec=1;maxaveragebitrate=128000"}
	if err := media.RegisterCodec(webrtc.RTPCodecParameters{RTPCodecCapability: codec, PayloadType: 111}, webrtc.RTPCodecTypeAudio); err != nil {
		t.Fatal(err)
	}
	api := webrtc.NewAPI(webrtc.WithMediaEngine(media))
	sender, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	receiver, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()
	if _, err = sender.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 99; i++ {
		track, err := webrtc.NewTrackLocalStaticRTP(codec, fmt.Sprintf("audio-%d", i), "voice-1")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = sender.AddTrack(track); err != nil {
			t.Fatal(err)
		}
	}
	offer, err := sender.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = sender.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	if err = receiver.SetRemoteDescription(offer); err != nil {
		t.Fatal(err)
	}
	answer, err := receiver.CreateAnswer(nil)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(map[string]any{"type": "voice.webrtc_answer", "data": map[string]string{"sdp": answer.SDP}})
	if err != nil {
		t.Fatal(err)
	}
	if len(wire) > maxIncomingEventSize {
		t.Fatalf("100-participant answer is %d bytes, limit is %d", len(wire), maxIncomingEventSize)
	}
}

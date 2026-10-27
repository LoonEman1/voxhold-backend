package voice

import (
	"strings"
	"testing"

	"github.com/pion/webrtc/v4"
)

type deferredSink struct{ offers []string }

func (s *deferredSink) SendOffer(_ string, sdp string) bool {
	s.offers = append(s.offers, sdp)
	return true
}
func (*deferredSink) SendICECandidate(string, ICECandidate) bool { return true }
func (*deferredSink) CloseVoice(string, string)                  {}

func TestDeferredNegotiationIncludesLatestTracks(t *testing.T) {
	for _, restart := range []bool{false, true} {
		t.Run(map[bool]string{false: "tracks", true: "tracks-and-restart"}[restart], func(t *testing.T) {
			peer, err := webrtc.NewPeerConnection(webrtc.Configuration{})
			if err != nil {
				t.Fatal(err)
			}
			remote, err := webrtc.NewPeerConnection(webrtc.Configuration{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = remote.Close(); _ = peer.Close() })
			if _, err := peer.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
				t.Fatal(err)
			}
			sink := &deferredSink{}
			r := newRoom(1)
			s := newSession(&Manager{sink: sink, maxAudioBitrateKbps: 128}, r, peer, "listener", 1, 1, 1, false, false)
			t.Cleanup(s.close)
			if err := s.synchronizeTracks(true); err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{"removed", "kept"} {
				track, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, id, "voice")
				if err != nil {
					t.Fatal(err)
				}
				r.tracks[id] = roomTrack{ownerConnectionID: "speaker", track: track}
				if err := s.synchronizeTracks(false); err != nil {
					t.Fatal(err)
				}
			}
			delete(r.tracks, "removed")
			if err := s.synchronizeTracks(false); err != nil {
				t.Fatal(err)
			}
			if restart {
				if err := s.restartICE(); err != nil {
					t.Fatal(err)
				}
			}
			if len(sink.offers) != 1 {
				t.Fatalf("offers before answer: %d", len(sink.offers))
			}
			answerVoiceOffer(t, s, remote, sink.offers[0])
			if len(sink.offers) != 2 {
				t.Fatalf("deferred offer lost: got %d offers", len(sink.offers))
			}
			final := sink.offers[1]
			if !strings.Contains(final, " kept") || strings.Contains(final, " removed") {
				t.Fatal("deferred SDP does not reflect final tracks")
			}
			if restart && testICEUfrag(final) == testICEUfrag(sink.offers[0]) {
				t.Fatal("pending ICE restart was lost")
			}
			answerVoiceOffer(t, s, remote, final)
			if len(sink.offers) != 2 {
				t.Fatal("unnecessary offer after settling")
			}
			s.selfDeaf.Store(true)
			if err := s.synchronizeTracks(false); err != nil {
				t.Fatal(err)
			}
			answerVoiceOffer(t, s, remote, sink.offers[2])
			if len(sink.offers) != 3 {
				t.Fatal("self-deaf created an offer storm")
			}
		})
	}
}

func answerVoiceOffer(t *testing.T, s *session, remote *webrtc.PeerConnection, offer string) {
	t.Helper()
	if err := remote.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offer}); err != nil {
		t.Fatal(err)
	}
	answer, err := remote.CreateAnswer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.SetLocalDescription(answer); err != nil {
		t.Fatal(err)
	}
	if err := s.acceptAnswer(answer.SDP); err != nil {
		t.Fatal(err)
	}
}

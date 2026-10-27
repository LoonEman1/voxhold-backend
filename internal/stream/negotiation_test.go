package stream

import (
	"strings"
	"testing"

	"github.com/pion/webrtc/v4"
)

func TestDeferredViewerNegotiationIncludesNewAudio(t *testing.T) {
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
			sink := &testSink{}
			r := newRoom(1, CodecVP8)
			r.publisher = &session{}
			videoCodec := webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000}
			video := newRelayTrack(videoCodec, "video", "stream", legacySDRRendition(CodecVP8))
			r.tracks[roomTrackKey{renditionID: "sdr", kind: webrtc.RTPCodecTypeVideo}] = roomTrack{track: video, codec: videoCodec, key: roomTrackKey{renditionID: "sdr", kind: webrtc.RTPCodecTypeVideo}, rendition: legacySDRRendition(CodecVP8)}
			s := newSession(&Manager{sink: sink, videoCodecDescriptors: streamVideoCodecDescriptors()}, r, peer, "viewer", 2, 1, 1, sessionViewer)
			s.selectedRenditionID = "sdr"
			t.Cleanup(s.close)
			if err := s.synchronizeViewerTracks(true); err != nil {
				t.Fatal(err)
			}
			initial := sink.offer("viewer")
			audioCodec := webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2, SDPFmtpLine: "minptime=10;useinbandfec=1"}
			r.tracks[roomTrackKey{kind: webrtc.RTPCodecTypeAudio}] = roomTrack{key: roomTrackKey{kind: webrtc.RTPCodecTypeAudio}, track: newRelayTrack(audioCodec, "audio", "stream", Rendition{}), codec: audioCodec}
			if err := s.synchronizeViewerTracks(false); err != nil {
				t.Fatal(err)
			}
			if restart {
				if err := s.restartICE(); err != nil {
					t.Fatal(err)
				}
			}
			answerStreamOffer(t, s, remote, initial)
			final := sink.offer("viewer")
			if final == initial || !strings.Contains(final, "m=audio") {
				t.Fatal("deferred audio offer was lost")
			}
			if restart && iceUfrag(final) == iceUfrag(initial) {
				t.Fatal("pending ICE restart was lost")
			}
			answerStreamOffer(t, s, remote, final)
			if sink.offer("viewer") != final {
				t.Fatal("offer storm after settling")
			}
			if err := s.synchronizeViewerTracks(true); err != nil {
				t.Fatal(err)
			}
			beforeRestart := sink.offer("viewer")
			if err := s.restartICE(); err != nil {
				t.Fatal(err)
			}
			answerStreamOffer(t, s, remote, beforeRestart)
			restarted := sink.offer("viewer")
			if iceUfrag(restarted) == iceUfrag(beforeRestart) {
				t.Fatal("ICE-only deferred restart was lost")
			}
			answerStreamOffer(t, s, remote, restarted)
			clear(r.tracks)
			if err := s.synchronizeViewerTracks(false); err != nil {
				t.Fatal(err)
			}
			if sink.offer("viewer") == restarted {
				t.Fatal("removing the last tracks did not renegotiate")
			}
		})
	}
}

func answerStreamOffer(t *testing.T, s *session, remote *webrtc.PeerConnection, offer string) {
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

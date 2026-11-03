package stream

import (
	"fmt"
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

// Firefox 159 answers AV1 with its receive level (9), while Chromium
// publishers advertise level 5. Opus receive preferences differ as well.
// Exercise real Pion negotiation, not just the codec comparison helper.
func TestAV1ViewerNegotiatesFirefoxAnswer(t *testing.T) {
	peer, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	remote, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		_ = peer.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = remote.Close(); _ = peer.Close() })
	sink := &testSink{}
	r := newRoom(1, CodecAV1)
	r.publisher = &session{}
	for _, codec := range []webrtc.RTPCodecCapability{
		{MimeType: webrtc.MimeTypeAV1, ClockRate: 90000, SDPFmtpLine: "level-idx=5;profile=0;tier=0"},
		{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2, SDPFmtpLine: "minptime=10;useinbandfec=1;stereo=1;sprop-stereo=1;maxaveragebitrate=128000"},
	} {
		key := roomTrackKey{kind: codecType(codec.MimeType)}
		rendition := Rendition{}
		if key.kind == webrtc.RTPCodecTypeVideo {
			key.renditionID = "sdr"
			rendition = legacySDRRendition(CodecAV1)
		}
		r.tracks[key] = roomTrack{key: key, codec: codec, track: newRelayTrack(codec, codec.MimeType, "av1-test", rendition), rendition: rendition}
	}
	s := newSession(&Manager{sink: sink, videoCodecDescriptors: streamVideoCodecDescriptors()}, r, peer, "viewer-firefox", 2, 1, 1, sessionViewer)
	s.selectedRenditionID = "sdr"
	t.Cleanup(s.close)
	if err := s.synchronizeViewerTracks(true); err != nil {
		t.Fatal(err)
	}
	if err := remote.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: sink.offer("viewer-firefox")}); err != nil {
		t.Fatal(err)
	}
	answer, err := remote.CreateAnswer(nil)
	if err != nil {
		t.Fatal(err)
	}
	answer.SDP = strings.ReplaceAll(answer.SDP, "level-idx=5;profile=0;tier=0", "profile=0;level-idx=9;tier=0")
	answer.SDP = strings.ReplaceAll(answer.SDP, "minptime=10;useinbandfec=1;stereo=1;sprop-stereo=1;maxaveragebitrate=128000", "maxplaybackrate=48000;stereo=1;useinbandfec=1")
	if !strings.Contains(answer.SDP, "profile=0;level-idx=9;tier=0") {
		t.Fatal("fixture has no Firefox AV1 receive parameters")
	}
	if err := s.acceptAnswer(answer.SDP); err != nil {
		t.Fatalf("Firefox AV1 + Opus answer rejected: %v", err)
	}
}

func TestViewerNegotiatesFirefoxVideoFormats(t *testing.T) {
	for _, test := range []struct {
		codec                           Codec
		mime, publisherFMTP, viewerFMTP string
	}{
		{CodecVP8, webrtc.MimeTypeVP8, "", "max-fs=12288;max-fr=60"},
		{CodecVP9, webrtc.MimeTypeVP9, "profile-id=0", "max-fs=12288;max-fr=60"},
		{CodecH264, webrtc.MimeTypeH264, "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f", "profile-level-id=42e01f;level-asymmetry-allowed=1;packetization-mode=1"},
	} {
		t.Run(string(test.codec), func(t *testing.T) {
			peer, err := webrtc.NewPeerConnection(webrtc.Configuration{})
			if err != nil {
				t.Fatal(err)
			}
			remote, err := webrtc.NewPeerConnection(webrtc.Configuration{})
			if err != nil {
				_ = peer.Close()
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = remote.Close(); _ = peer.Close() })
			sink := &testSink{}
			r := newRoom(1, test.codec)
			r.publisher = &session{}
			codec := webrtc.RTPCodecCapability{MimeType: test.mime, ClockRate: 90000, SDPFmtpLine: test.publisherFMTP}
			key := roomTrackKey{renditionID: "sdr", kind: webrtc.RTPCodecTypeVideo}
			rendition := legacySDRRendition(test.codec)
			r.tracks[key] = roomTrack{key: key, codec: codec, track: newRelayTrack(codec, "video", "firefox-test", rendition), rendition: rendition}
			s := newSession(&Manager{sink: sink, videoCodecDescriptors: streamVideoCodecDescriptors()}, r, peer, "viewer", 2, 1, 1, sessionViewer)
			s.selectedRenditionID = "sdr"
			t.Cleanup(s.close)
			if err := s.synchronizeViewerTracks(true); err != nil {
				t.Fatal(err)
			}
			if err := remote.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: sink.offer("viewer")}); err != nil {
				t.Fatal(err)
			}
			answer, err := remote.CreateAnswer(nil)
			if err != nil {
				t.Fatal(err)
			}
			preferences := s.manager.viewerCodecPreferences(codec)
			prefix := fmt.Sprintf("a=fmtp:%d ", preferences[0].PayloadType)
			if test.publisherFMTP == "" {
				rtpmap := fmt.Sprintf("a=rtpmap:%d VP8/90000\r\n", preferences[0].PayloadType)
				answer.SDP = strings.Replace(answer.SDP, rtpmap, rtpmap+prefix+test.viewerFMTP+"\r\n", 1)
			} else {
				answer.SDP = strings.Replace(answer.SDP, prefix+test.publisherFMTP, prefix+test.viewerFMTP, 1)
			}
			if !strings.Contains(answer.SDP, prefix+test.viewerFMTP) {
				t.Fatal("missing Firefox FMTP fixture")
			}
			if err := s.acceptAnswer(answer.SDP); err != nil {
				t.Fatalf("Firefox %s answer rejected: %v", test.codec, err)
			}
		})
	}
}

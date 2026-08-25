package stream

import (
	"bytes"
	"testing"

	"github.com/pion/interceptor"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

func TestRoomSnapshotReturnsOnlySelectedVideoRendition(t *testing.T) {
	sdr := legacySDRRendition(CodecVP9)
	hdr := Rendition{
		ID: "hdr", Codec: CodecVP9, Profile: "2", DynamicRange: "hdr10",
		BitDepth: 10, ColorPrimaries: "bt2020", Transfer: "pq", Matrix: "bt2020-ncl",
	}
	room := newRoomWithRenditions(1, []Rendition{sdr, hdr})
	room.publisher = &session{}
	sdrTrack := newRelayTrack(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP9}, "sdr", "stream", sdr)
	hdrTrack := newRelayTrack(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP9}, "hdr", "stream", hdr)
	audioTrack := newRelayTrack(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus}, "audio", "stream", Rendition{})
	for _, value := range []roomTrack{
		{key: roomTrackKey{renditionID: "sdr", kind: webrtc.RTPCodecTypeVideo}, track: sdrTrack, rendition: sdr},
		{key: roomTrackKey{renditionID: "hdr", kind: webrtc.RTPCodecTypeVideo}, track: hdrTrack, rendition: hdr},
		{key: roomTrackKey{kind: webrtc.RTPCodecTypeAudio}, track: audioTrack},
	} {
		if !room.addTrack(value) {
			t.Fatalf("failed to add track %+v", value.key)
		}
	}

	snapshot := room.trackSnapshot("hdr")
	if len(snapshot) != 2 ||
		snapshot[roomTrackKey{renditionID: "hdr", kind: webrtc.RTPCodecTypeVideo}].track != hdrTrack ||
		snapshot[roomTrackKey{kind: webrtc.RTPCodecTypeAudio}].track != audioTrack {

		t.Fatalf("unexpected HDR viewer tracks: %#v", snapshot)
	}
	if _, leaked := snapshot[roomTrackKey{renditionID: "sdr", kind: webrtc.RTPCodecTypeVideo}]; leaked {
		t.Fatal("SDR track leaked into HDR viewer snapshot")
	}
}

func TestRelayTrackRemapsColorSpaceExtensionPerViewer(t *testing.T) {
	rendition := Rendition{
		ID: "hdr", Codec: CodecVP9, Profile: "2", DynamicRange: "hdr10",
		BitDepth: 10, ColorPrimaries: "bt2020", Transfer: "pq", Matrix: "bt2020-ncl",
	}
	track := newRelayTrack(
		webrtc.RTPCodecCapability{
			MimeType: webrtc.MimeTypeVP9, ClockRate: 90000, SDPFmtpLine: "profile-id=2",
		},
		"hdr", "stream", rendition,
	)
	first := &captureTrackWriter{}
	second := &captureTrackWriter{}
	for _, context := range []*testTrackLocalContext{
		newTestTrackLocalContext("first", 100, 120, 3, first),
		newTestTrackLocalContext("second", 200, 121, 7, second),
	} {
		if _, err := track.Bind(context); err != nil {
			t.Fatal(err)
		}
	}

	packet := &rtp.Packet{Header: rtp.Header{Marker: true, SSRC: 999, PayloadType: 98}, Payload: []byte{1}}
	if err := packet.Header.SetExtension(13, []byte{0xff}); err != nil {
		t.Fatal(err)
	}
	if err := track.WriteRTP(packet); err != nil {
		t.Fatal(err)
	}

	assertRemappedColorExtension(t, first.header, 100, 120, 3)
	assertRemappedColorExtension(t, second.header, 200, 121, 7)
	if first.header.GetExtension(13) != nil || second.header.GetExtension(13) != nil {
		t.Fatal("publisher extension ID leaked to a viewer")
	}
}

func assertRemappedColorExtension(
	t *testing.T,
	header rtp.Header,
	ssrc uint32,
	payloadType uint8,
	extensionID uint8,
) {
	t.Helper()
	if header.SSRC != ssrc || header.PayloadType != payloadType {
		t.Fatalf("unexpected rewritten RTP header: %+v", header)
	}
	if !bytes.Equal(header.GetExtension(extensionID), []byte{9, 16, 9, 0x10}) {
		t.Fatalf("unexpected color-space extension: %v", header.GetExtension(extensionID))
	}
}

type captureTrackWriter struct {
	header rtp.Header
}

func (w *captureTrackWriter) WriteRTP(header *rtp.Header, _ []byte) (int, error) {
	w.header = *header
	w.header.Extensions = append([]rtp.Extension(nil), header.Extensions...)
	return 1, nil
}

func (*captureTrackWriter) Write(value []byte) (int, error) { return len(value), nil }

type testTrackLocalContext struct {
	id         string
	ssrc       webrtc.SSRC
	codec      webrtc.RTPCodecParameters
	extensions []webrtc.RTPHeaderExtensionParameter
	writer     webrtc.TrackLocalWriter
}

func newTestTrackLocalContext(
	id string,
	ssrc webrtc.SSRC,
	payloadType webrtc.PayloadType,
	extensionID int,
	writer webrtc.TrackLocalWriter,
) *testTrackLocalContext {
	return &testTrackLocalContext{
		id: id, ssrc: ssrc, writer: writer,
		codec: webrtc.RTPCodecParameters{
			RTPCodecCapability: webrtc.RTPCodecCapability{
				MimeType: webrtc.MimeTypeVP9, ClockRate: 90000, SDPFmtpLine: "profile-id=2",
			},
			PayloadType: payloadType,
		},
		extensions: []webrtc.RTPHeaderExtensionParameter{{
			URI: colorSpaceRTPHeaderExtensionURI, ID: extensionID,
		}},
	}
}

func (c *testTrackLocalContext) CodecParameters() []webrtc.RTPCodecParameters {
	return []webrtc.RTPCodecParameters{c.codec}
}
func (c *testTrackLocalContext) HeaderExtensions() []webrtc.RTPHeaderExtensionParameter {
	return c.extensions
}
func (c *testTrackLocalContext) SSRC() webrtc.SSRC                     { return c.ssrc }
func (*testTrackLocalContext) SSRCRetransmission() webrtc.SSRC         { return 0 }
func (*testTrackLocalContext) SSRCForwardErrorCorrection() webrtc.SSRC { return 0 }
func (c *testTrackLocalContext) WriteStream() webrtc.TrackLocalWriter  { return c.writer }
func (c *testTrackLocalContext) ID() string                            { return c.id }
func (*testTrackLocalContext) RTCPReader() interceptor.RTCPReader      { return nil }

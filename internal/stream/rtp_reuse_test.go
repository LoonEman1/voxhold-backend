package stream

import (
	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/nack"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"testing"
	"time"
)

type interceptorTrackWriter struct{ writer interceptor.RTPWriter }

func (w interceptorTrackWriter) WriteRTP(header *rtp.Header, payload []byte) (int, error) {
	return w.writer.Write(header, payload, nil)
}
func (w interceptorTrackWriter) Write(data []byte) (int, error) { return len(data), nil }

func TestReusedReceiveBufferPreservesRelayAndNACKPayloads(t *testing.T) {
	factory, err := nack.NewResponderInterceptor()
	if err != nil {
		t.Fatal(err)
	}
	responder, err := factory.NewInterceptor("test")
	if err != nil {
		t.Fatal(err)
	}
	defer responder.Close()
	type seen struct {
		sequence uint16
		payload  string
	}
	received := make(chan seen, 3)
	writer := responder.BindLocalStream(&interceptor.StreamInfo{SSRC: 42, PayloadType: 96, RTCPFeedback: []interceptor.RTCPFeedback{{Type: "nack"}}}, interceptor.RTPWriterFunc(func(header *rtp.Header, payload []byte, _ interceptor.Attributes) (int, error) {
		received <- seen{header.SequenceNumber, string(payload)}
		return len(payload), nil
	}))
	track := &relayTrack{bindings: []relayBinding{{ssrc: 42, payloadType: 96, writeStream: interceptorTrackWriter{writer}}}}
	buffer := make([]byte, 1500)
	var packet rtp.Packet
	for i, payload := range []string{"first", "other"} {
		wire, err := (&rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: uint16(i + 1), SSRC: 99}, Payload: []byte(payload)}).Marshal()
		if err != nil {
			t.Fatal(err)
		}
		n := copy(buffer, wire)
		packet = rtp.Packet{}
		if err := packet.Unmarshal(buffer[:n]); err != nil {
			t.Fatal(err)
		}
		if err := track.WriteRTP(&packet); err != nil {
			t.Fatal(err)
		}
	}
	feedback, err := rtcp.Marshal([]rtcp.Packet{&rtcp.TransportLayerNack{SenderSSRC: 7, MediaSSRC: 42, Nacks: []rtcp.NackPair{{PacketID: 1}}}})
	if err != nil {
		t.Fatal(err)
	}
	reader := responder.BindRTCPReader(interceptor.RTCPReaderFunc(func(buf []byte, _ interceptor.Attributes) (int, interceptor.Attributes, error) {
		return copy(buf, feedback), nil, nil
	}))
	if _, _, err := reader.Read(make([]byte, 1500), nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []seen{{1, "first"}, {2, "other"}, {1, "first"}} {
		select {
		case got := <-received:
			if got != want {
				t.Fatalf("got %+v want %+v", got, want)
			}
		case <-time.After(time.Second):
			t.Fatal("missing forwarded/retransmitted packet")
		}
	}
}

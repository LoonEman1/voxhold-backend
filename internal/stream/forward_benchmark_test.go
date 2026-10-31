package stream

import (
	"github.com/pion/rtp"
	"testing"
)

// Matches TrackRemote.ReadRTP's allocations, without network timing noise.
//
//go:noinline
func legacyForwardPacket(wire []byte) (*rtp.Packet, error) {
	buffer := make([]byte, 1500)
	n := copy(buffer, wire)
	packet := new(rtp.Packet)
	return packet, packet.Unmarshal(buffer[:n])
}

func BenchmarkStreamRelayWrite(b *testing.B) {
	wire, err := (&rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: 96, SequenceNumber: 1, SSRC: 1}, Payload: make([]byte, 1200)}).Marshal()
	if err != nil {
		b.Fatal(err)
	}
	track := &relayTrack{bindings: []relayBinding{{ssrc: 1, payloadType: 96, writeStream: discardRTPWriter{}}}}
	for _, reuse := range []bool{false, true} {
		name := "LegacyReadRTP"
		if reuse {
			name = "ReusedPacket"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			buffer := make([]byte, 1500)
			var packet rtp.Packet
			for b.Loop() {
				if reuse {
					n := copy(buffer, wire)
					packet = rtp.Packet{}
					if err := packet.Unmarshal(buffer[:n]); err != nil {
						b.Fatal(err)
					}
					if err := track.WriteRTP(&packet); err != nil {
						b.Fatal(err)
					}
				} else {
					parsed, err := legacyForwardPacket(wire)
					if err != nil {
						b.Fatal(err)
					}
					if err := track.WriteRTP(parsed); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}

type discardRTPWriter struct{}

func (discardRTPWriter) WriteRTP(_ *rtp.Header, payload []byte) (int, error) {
	return len(payload), nil
}
func (discardRTPWriter) Write(data []byte) (int, error) { return len(data), nil }

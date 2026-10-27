package stream

import (
	"errors"
	"testing"

	"github.com/pion/rtp"
)

type failingTrackWriter struct{}

func (failingTrackWriter) WriteRTP(*rtp.Header, []byte) (int, error) {
	return 0, errors.New("closed binding")
}
func (failingTrackWriter) Write([]byte) (int, error) { return 0, errors.New("closed binding") }

func TestRelayContinuesAfterOneBindingFails(t *testing.T) {
	healthy := &captureTrackWriter{}
	track := &relayTrack{bindings: []relayBinding{{id: "bad", writeStream: failingTrackWriter{}}, {id: "good", ssrc: 42, payloadType: 96, writeStream: healthy}}}
	if err := track.WriteRTP(&rtp.Packet{Header: rtp.Header{Version: 2}, Payload: []byte{1}}); err == nil {
		t.Fatal("write failure not reported")
	}
	if healthy.header.SSRC != 42 {
		t.Fatal("healthy binding missed packet")
	}
}

package telemetry

import (
	"errors"
	"testing"
)

func TestForwardCountersAndSampledTiming(t *testing.T) {
	Enable()
	before := Snapshot()["stream_rtp"]
	want := errors.New("write failed")
	for i := range 256 {
		err := Forward(StreamRTP, func() error {
			if i%2 == 0 {
				return want
			}
			return nil
		})
		if (err != nil) != (i%2 == 0) {
			t.Fatal("writer result changed")
		}
	}
	after := Snapshot()["stream_rtp"]
	if after.Count-before.Count != 256 || after.Errors-before.Errors != 128 || after.TimingSamples-before.TimingSamples != 2 {
		t.Fatalf("incorrect sampled counters: %+v", after)
	}
}

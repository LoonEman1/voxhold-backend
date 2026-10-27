package voice

import (
	"fmt"
	"sync"
	"testing"
)

func TestRepeatedConcurrentUnansweredJoinsReleaseResources(t *testing.T) {
	m := newTestManager(t, &deferredSink{})
	// Each caller has a distinct connection, as serialized by the WS handler.
	// Use a sink with synchronized offers; no SDP answer ever arrives.
	m.sink = &lifecycleSink{}
	var workers sync.WaitGroup
	for worker := range 4 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			id := fmt.Sprintf("worker-%d", worker)
			for range 25 {
				if err := m.Join(id, int64(worker+1), 1, 1, false, false); err != nil {
					t.Error(err)
					return
				}
				m.Leave(id)
			}
		}()
	}
	workers.Wait()
	if stats := m.Stats(); stats["rooms"] != 0 || stats["peers"] != 0 || stats["senders"] != 0 {
		t.Fatal("resources retained", stats)
	}
}

type lifecycleSink struct{}

func (*lifecycleSink) SendOffer(string, string) bool              { return true }
func (*lifecycleSink) SendICECandidate(string, ICECandidate) bool { return true }
func (*lifecycleSink) CloseVoice(string, string)                  {}

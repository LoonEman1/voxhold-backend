package stream

import (
	"fmt"
	"sync"
	"testing"
)

func TestRepeatedPublisherAndWaitingViewerCleanup(t *testing.T) {
	m, err := NewManager(Config{}, &testSink{})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	var workers sync.WaitGroup
	for worker := range 4 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			publisher, viewer := fmt.Sprintf("publisher-%d", worker), fmt.Sprintf("viewer-%d", worker)
			for range 25 {
				if err := m.Start(publisher, int64(worker*2+1), 1, int64(worker+1), CodecVP8, false); err != nil {
					t.Error(err)
					return
				}
				if err := m.Watch(viewer, int64(worker*2+2), 1, int64(worker+1)); err != nil {
					t.Error(err)
					return
				}
				m.Leave(viewer)
				m.Leave(publisher)
			}
		}()
	}
	workers.Wait()
	if stats := m.Stats(); stats["rooms"] != 0 || stats["peers"] != 0 || stats["senders"] != 0 {
		t.Fatal("resources retained", stats)
	}
}

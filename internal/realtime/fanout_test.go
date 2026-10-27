package realtime

import (
	"sync"
	"testing"
	"time"
)

type blockingFanoutCloser struct {
	entered, release chan struct{}
	once             sync.Once
}

func (c *blockingFanoutCloser) Leave(string) { c.once.Do(func() { close(c.entered); <-c.release }) }

func TestFanoutEnqueuesHealthyClientsBeforeCleanup(t *testing.T) {
	h := NewHub()
	bad, good := NewClient(1, "bad", []int64{1}), NewClient(2, "good", []int64{1})
	h.Register(bad)
	h.Register(good)
	h.JoinVoice(bad, 1, 1, false, false)
	drainOutgoingEvents(good)
	bad.Close()
	c := &blockingFanoutCloser{entered: make(chan struct{}), release: make(chan struct{})}
	h.SetVoiceSessionCloser(c)
	done := make(chan struct{})
	go func() { h.broadcastToClients([]*Client{bad, bad, good}, OutgoingEvent{Type: EventReady}); close(done) }()
	defer func() { close(c.release); <-done; h.Close() }()
	select {
	case <-c.entered:
	case <-time.After(time.Second):
		t.Fatal("cleanup not reached")
	}
	for {
		event, ok := good.PopOutgoing()
		if !ok {
			t.Fatal("healthy recipient delayed behind cleanup")
		}
		if event.Type == EventReady {
			break
		}
	}
}

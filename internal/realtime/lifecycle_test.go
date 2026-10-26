package realtime

import (
	"sync/atomic"
	"testing"
	"time"
	"voxhold-backend/internal/voice"
)

func TestHubRejectsRegistrationAfterShutdownAndClosedSubscriptions(t *testing.T) {
	h := NewHub()
	c := NewClient(1, "session", []int64{1})
	if !h.Register(c) {
		t.Fatal("register")
	}
	h.Unregister(c)
	if h.Subscribe(c, 1, 1) {
		t.Fatal("closed client subscribed")
	}
	h.Close()
	if h.Register(NewClient(2, "two", []int64{1})) {
		t.Fatal("registered after shutdown")
	}
}

func TestVoiceFailureHandlerOnlyClosesItsLogicalJoin(t *testing.T) {
	h := NewHub()
	c := NewClient(1, "session", []int64{1})
	h.Register(c)
	defer h.Unregister(c)
	h.JoinVoice(c, 1, 1, false, false)
	sink := NewVoiceSignalSink(h)
	oldFailure := sink.SessionFailureHandler(c.ConnectionID())
	h.JoinVoice(c, 1, 1, false, false)
	oldFailure("old peer failed")
	if !h.HasVoiceParticipant(1, 1, 1) {
		t.Fatal("old failure removed replacement")
	}
	sink.SessionFailureHandler(c.ConnectionID())("current peer failed")
	if h.HasVoiceParticipant(1, 1, 1) {
		t.Fatal("current failed join remains")
	}
}

func TestStreamFailureHandlerOnlyClosesItsLogicalWatch(t *testing.T) {
	h := NewHub()
	p := NewClient(1, "one", []int64{1})
	v := NewClient(2, "two", []int64{1})
	h.Register(p)
	h.Register(v)
	defer h.Close()
	h.JoinVoice(p, 1, 1, false, false)
	h.JoinVoice(v, 1, 1, false, false)
	if _, err := h.StartStream(p, 1, 1, StreamModeServer, StreamCodecVP8, false); err != nil {
		t.Fatal(err)
	}
	if _, err := h.WatchStream(v, 1, 1); err != nil {
		t.Fatal(err)
	}
	sink := NewStreamSignalSink(h)
	oldFailure := sink.SessionFailureHandler(v.ConnectionID())
	h.LeaveStream(v, "rewatch")
	if _, err := h.WatchStream(v, 1, 1); err != nil {
		t.Fatal(err)
	}
	oldFailure("old peer failed")
	if _, exists := h.streams.byClient[v]; !exists {
		t.Fatal("old failure removed new viewer")
	}
	sink.SessionFailureHandler(v.ConnectionID())("current peer failed")
	if _, exists := h.streams.byClient[v]; exists {
		t.Fatal("current failed viewer remains")
	}
}

type delayedVoiceFailureSink struct {
	delegate               *VoiceSignalSink
	offers                 atomic.Int64
	entered, release, done chan struct{}
}

func (s *delayedVoiceFailureSink) SendOffer(string, string) bool                  { return s.offers.Add(1) > 1 }
func (*delayedVoiceFailureSink) SendICECandidate(string, voice.ICECandidate) bool { return true }
func (s *delayedVoiceFailureSink) CloseVoice(id, reason string)                   { s.delegate.CloseVoice(id, reason) }
func (s *delayedVoiceFailureSink) SessionFailureHandler(id string) func(string) {
	handle := s.delegate.SessionFailureHandler(id)
	return func(reason string) { close(s.entered); <-s.release; handle(reason); close(s.done) }
}

func TestDelayedPeerFailureKeepsReplacementMediaAlive(t *testing.T) {
	h := NewHub()
	sink := &delayedVoiceFailureSink{delegate: NewVoiceSignalSink(h), entered: make(chan struct{}), release: make(chan struct{}), done: make(chan struct{})}
	m, err := voice.NewManager(voice.Config{}, sink)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	h.SetVoiceSessionCloser(m)
	c := NewClient(1, "one", []int64{1})
	h.Register(c)
	defer h.Unregister(c)
	h.JoinVoice(c, 1, 1, false, false)
	if err = m.Join(c.ConnectionID(), 1, 1, 1, false, false); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sink.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("old failure did not reach callback")
	}
	h.JoinVoice(c, 1, 1, false, false)
	if err = m.Join(c.ConnectionID(), 1, 1, 1, false, false); err != nil {
		t.Fatal(err)
	}
	close(sink.release)
	select {
	case <-sink.done:
	case <-time.After(2 * time.Second):
		t.Fatal("failure callback stuck")
	}
	if err = m.SetState(c.ConnectionID(), false, false); err != nil || !h.HasVoiceParticipant(1, 1, 1) {
		t.Fatalf("replacement was removed: %v", err)
	}
}

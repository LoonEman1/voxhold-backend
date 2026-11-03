package stream

import (
	"errors"
	"testing"
)

func TestSFUSeparatesPublishersSharingAChannel(t *testing.T) {
	manager, err := NewManager(Config{UDPPort: 0}, &testSink{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	for i, name := range []string{"first", "second"} {
		if err := manager.Start(name, int64(i+1), 10, 100, CodecVP8, false); err != nil {
			t.Fatal(err)
		}
	}
	for i, name := range []string{"first", "second"} {
		if err := manager.WatchRenditionForPublisher("viewer-"+name, int64(i+3), 10, 100, name, "sdr"); err != nil {
			t.Fatal(err)
		}
		if manager.session("viewer-"+name).room != manager.session(name).room {
			t.Fatal("viewer routed to another publisher")
		}
	}
	for _, test := range []struct {
		server, channel int64
		publisher       string
	}{{10, 100, "unknown"}, {11, 100, "first"}, {10, 101, "first"}} {
		if err := manager.WatchRenditionForPublisher("outsider", 9, test.server, test.channel, test.publisher, "sdr"); !errors.Is(err, ErrStreamNotFound) {
			t.Fatalf("foreign publisher accepted: %v", err)
		}
	}
	second := manager.session("second")
	viewer := manager.session("viewer-second")
	manager.Leave("first")
	if manager.session("first") != nil || manager.session("viewer-first") != nil {
		t.Fatal("first stream did not close")
	}
	if manager.session("second") != second || manager.session("viewer-second") != viewer || second.closed.Load() || viewer.closed.Load() {
		t.Fatal("closing first stream interrupted second")
	}
	if err := manager.Start("third", 5, 10, 100, CodecVP9, false); err != nil {
		t.Fatal(err)
	}
}

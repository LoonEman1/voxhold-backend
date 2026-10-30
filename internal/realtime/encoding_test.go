package realtime

import (
	"bytes"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
)

type countedPayload struct{ calls *atomic.Int32 }

func (p countedPayload) MarshalJSON() ([]byte, error) {
	p.calls.Add(1)
	return []byte(`{"content":"<safe>&"}`), nil
}

func TestBroadcastEncodesOnceAcrossConcurrentWriters(t *testing.T) {
	var calls atomic.Int32
	event := OutgoingEvent{Type: EventMessageCreated, Data: countedPayload{&calls}}.shareEncoding()
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			data, shared, err := event.SharedJSON()
			if err != nil || !shared || !json.Valid(data) || bytes.Contains(data, []byte("<safe>")) {
				t.Errorf("invalid shared JSON %s: shared=%v err=%v", data, shared, err)
			}
		})
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("payload encoded %d times", calls.Load())
	}
}

func TestFilteredSnapshotsCannotReusePrivateBroadcastBytes(t *testing.T) {
	root := OutgoingEvent{Type: EventVoiceSnapshot, Data: VoiceSnapshotData{Participants: []VoiceParticipantData{{ChannelID: 1, UserID: 1}, {ChannelID: 2, UserID: 2}}}}.shareEncoding()
	if _, _, err := root.SharedJSON(); err != nil {
		t.Fatal(err)
	}
	for _, channel := range []int64{1, 2} {
		client := NewClient(channel, "fixture", nil)
		client.replaceChannelAccess(map[int64]struct{}{channel: {}})
		filtered, allowed := client.filterChannelEvent(root)
		if !allowed {
			t.Fatal("snapshot rejected")
		}
		if _, shared, _ := filtered.SharedJSON(); shared {
			t.Fatal("filtered snapshot retained broadcast cache")
		}
		data := filtered.Data.(VoiceSnapshotData)
		if len(data.Participants) != 1 || data.Participants[0].ChannelID != channel {
			t.Fatalf("private data leaked: %#v", data)
		}
	}
}

func TestSharedEncodingMatchesExistingWireFormat(t *testing.T) {
	event := OutgoingEvent{Type: EventError, RequestID: "request", Data: map[string]string{"content": "<text>\u2028"}}
	var want bytes.Buffer
	if err := json.NewEncoder(&want).Encode(event); err != nil {
		t.Fatal(err)
	}
	got, shared, err := event.shareEncoding().SharedJSON()
	if err != nil || !shared || !bytes.Equal(got, want.Bytes()) {
		t.Fatalf("got %s, %v; want %s", got, err, want.Bytes())
	}
}

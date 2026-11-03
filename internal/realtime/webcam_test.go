package realtime

import "testing"

func TestWebcamStateSurvivesAudioUpdatesAndResetsOnMove(t *testing.T) {
	hub := NewHub()
	client := NewClient(1, "camera", []int64{10})
	hub.Register(client)
	defer hub.Unregister(client)
	if _, ok := hub.JoinVoice(client, 10, 100, false, false); !ok {
		t.Fatal("join")
	}
	if state, ok := hub.UpdateVoiceState(client, false, false, true); !ok || !state.SelfVideo {
		t.Fatal("camera was not enabled")
	}
	if state, ok := hub.UpdateVoiceState(client, true, true); !ok || !state.SelfVideo {
		t.Fatal("legacy audio update disabled camera")
	}
	if state, ok := hub.JoinVoice(client, 10, 100, false, false); !ok || !state.Participant.SelfVideo {
		t.Fatal("same-room join lost camera")
	}
	if state, ok := hub.UpdateVoiceState(client, false, false, false); !ok || state.SelfVideo {
		t.Fatal("camera was not disabled")
	}
	hub.UpdateVoiceState(client, false, false, true)
	if state, ok := hub.JoinVoice(client, 10, 101, false, false); !ok || state.Participant.SelfVideo {
		t.Fatal("new room inherited camera")
	}
}

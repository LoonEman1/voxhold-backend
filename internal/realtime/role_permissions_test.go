package realtime

import "testing"

func TestRefreshingPermissionsDoesNotCloseVoiceForAnIdleReadOnlyMember(t *testing.T) {
	hub := NewHub()
	defer hub.Close()
	client := NewClient(2, "reader", []int64{1})
	hub.Register(client)
	drainOutgoingEvents(client)
	hub.SetRolePermissionLoader(func(int64, int64) (int, error) { return 1, nil })
	hub.RefreshRolePermissions(1)
	for {
		event, ok := client.PopOutgoing()
		if !ok {
			break
		}
		if event.Type == EventVoiceWebRTCClosed {
			t.Fatal("idle reader received a voice teardown")
		}
	}
}

func TestRoleChangesPreserveAllowedMediaAndRevokeOnlyLostPermission(t *testing.T) {
	hub := NewHub()
	defer hub.Close()
	client := NewClient(2, "member", []int64{1})
	if !hub.Register(client) {
		t.Fatal("register")
	}
	bits := 15
	hub.SetRolePermissionLoader(func(int64, int64) (int, error) { return bits, nil })
	if _, ok := hub.JoinVoice(client, 1, 1, false, false); !ok {
		t.Fatal("join")
	}
	if _, err := hub.StartStream(client, 1, 1, StreamModeServer, StreamCodecVP8, false); err != nil {
		t.Fatal(err)
	}
	hub.RefreshRolePermissions(1)
	if _, ok := hub.voice.current(client); !ok || !hub.streams.isPublisher(client) {
		t.Fatal("unchanged rights interrupted media")
	}
	bits = 7
	hub.RefreshRolePermissions(1)
	if _, ok := hub.voice.current(client); !ok {
		t.Fatal("stream revocation removed voice")
	}
	if hub.streams.isPublisher(client) {
		t.Fatal("revoked stream still running")
	}
	bits = 3
	hub.RefreshRolePermissions(1)
	if _, ok := hub.voice.current(client); ok {
		t.Fatal("revoked voice still running")
	}
}

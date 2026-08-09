package realtime

import "testing"

func TestHubRevokeSession(t *testing.T) {
	hub := NewHub()
	client := NewClient(1, "session-token")

	if !hub.Register(client) {
		t.Fatal("register client")
	}

	if !hub.Subscribe(client, 10, 100) {
		t.Fatal("subscribe client")
	}

	hub.RevokeSession("session-token")

	select {
	case <-client.Done():
	default:
		t.Fatal("revoked client is still active")
	}

	if delivered := hub.Publish(
		100,
		OutgoingEvent{Type: EventMessageCreated},
	); delivered != 0 {
		t.Fatalf(
			"message delivered to revoked session: %d",
			delivered,
		)
	}
}

func TestHubRevokeUserFromServer(t *testing.T) {
	hub := NewHub()
	client := NewClient(1, "session-token")

	if !hub.Register(client) {
		t.Fatal("register client")
	}

	hub.Subscribe(client, 10, 100)
	hub.Subscribe(client, 20, 200)

	hub.RevokeUserFromServer(1, 10)

	if delivered := hub.Publish(
		100,
		OutgoingEvent{Type: EventMessageCreated},
	); delivered != 0 {
		t.Fatalf(
			"message delivered from revoked server: %d",
			delivered,
		)
	}

	if delivered := hub.Publish(
		200,
		OutgoingEvent{Type: EventMessageCreated},
	); delivered != 1 {
		t.Fatalf(
			"unrelated server subscription was removed: %d",
			delivered,
		)
	}
}

func TestHubRevokeServer(t *testing.T) {
	hub := NewHub()
	firstClient := NewClient(1, "first-session")
	secondClient := NewClient(2, "second-session")

	hub.Register(firstClient)
	hub.Register(secondClient)
	hub.Subscribe(firstClient, 10, 100)
	hub.Subscribe(secondClient, 10, 100)

	hub.RevokeServer(10)

	if delivered := hub.Publish(
		100,
		OutgoingEvent{Type: EventMessageCreated},
	); delivered != 0 {
		t.Fatalf(
			"message delivered after server revocation: %d",
			delivered,
		)
	}
}

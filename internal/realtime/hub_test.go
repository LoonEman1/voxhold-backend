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
	observer := NewClient(2, "observer-session")

	if !hub.Register(client) {
		t.Fatal("register client")
	}
	if !hub.Register(observer) {
		t.Fatal("register observer")
	}

	hub.Subscribe(client, 10, 100)
	hub.Subscribe(client, 20, 200)
	hub.Subscribe(observer, 10, 100)

	hub.RevokeUserFromServer(1, 10)

	assertMemberRemovedEvent(t, client, 10, 1)
	assertMemberRemovedEvent(t, observer, 10, 1)

	if delivered := hub.Publish(
		100,
		OutgoingEvent{Type: EventMessageCreated},
	); delivered != 1 {
		t.Fatalf(
			"unexpected delivery count after member removal: %d",
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
	hub.Subscribe(firstClient, 10, 101)
	hub.Subscribe(secondClient, 10, 100)

	hub.RevokeServer(10)

	assertServerDeletedEvent(t, firstClient, 10)
	assertNoOutgoingEvent(t, firstClient)
	assertServerDeletedEvent(t, secondClient, 10)

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

func assertMemberRemovedEvent(
	t *testing.T,
	client *Client,
	serverID int64,
	userID int64,
) {
	t.Helper()

	event := nextOutgoingEvent(t, client)
	if event.Type != EventServerMemberRemoved {
		t.Fatalf("unexpected event type: %s", event.Type)
	}

	data, ok := event.Data.(ServerMemberRemovedData)
	if !ok {
		t.Fatalf("unexpected event data: %T", event.Data)
	}

	if data.ServerID != serverID || data.UserID != userID {
		t.Fatalf("unexpected event data: %+v", data)
	}
}

func assertServerDeletedEvent(
	t *testing.T,
	client *Client,
	serverID int64,
) {
	t.Helper()

	event := nextOutgoingEvent(t, client)
	if event.Type != EventServerDeleted {
		t.Fatalf("unexpected event type: %s", event.Type)
	}

	data, ok := event.Data.(ServerDeletedData)
	if !ok {
		t.Fatalf("unexpected event data: %T", event.Data)
	}

	if data.ServerID != serverID {
		t.Fatalf("unexpected event data: %+v", data)
	}
}

func nextOutgoingEvent(
	t *testing.T,
	client *Client,
) OutgoingEvent {
	t.Helper()

	select {
	case event := <-client.Outgoing():
		return event
	default:
		t.Fatal("expected outgoing event")
		return OutgoingEvent{}
	}
}

func assertNoOutgoingEvent(
	t *testing.T,
	client *Client,
) {
	t.Helper()

	select {
	case event := <-client.Outgoing():
		t.Fatalf("unexpected outgoing event: %s", event.Type)
	default:
	}
}

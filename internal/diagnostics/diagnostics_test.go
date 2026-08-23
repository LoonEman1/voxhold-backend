package diagnostics

import (
	"context"
	"strings"
	"testing"
	"time"
)

type repositoryStub struct {
	input BatchInput
	owner bool
}

func (r *repositoryStub) InsertBatch(
	_ context.Context,
	_ int64,
	input BatchInput,
) (int, error) {
	r.input = input
	return len(input.Events), nil
}

func (*repositoryStub) List(
	context.Context,
	ListFilter,
) ([]Event, error) {
	return []Event{{ID: 1}}, nil
}

func (r *repositoryStub) IsOwner(
	context.Context,
	int64,
) (bool, error) {
	return r.owner, nil
}

func TestServiceSanitizesClientDiagnosticDetails(t *testing.T) {
	repository := &repositoryStub{}
	service := NewService(repository)
	now := time.Unix(1_800_000_000, 0)
	service.now = func() time.Time { return now }

	accepted, err := service.Ingest(
		context.Background(),
		10,
		BatchInput{
			SessionID:     "session-1",
			ClientVersion: "1.0.3",
			Platform:      strings.Repeat("browser", 40),
			Events: []EventInput{{
				ClientTimestampMS: now.UnixMilli(),
				Category:          "webrtc",
				Name:              "voice.stats",
				Level:             "info",
				DetailsJSON:       `{"bytes_received":100,"sdp_bytes":123,"token":"secret","candidate":"1.2.3.4"}`,
			}},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if accepted != 1 {
		t.Fatalf("accepted = %d, want 1", accepted)
	}
	if len(repository.input.Platform) > 200 {
		t.Fatalf("platform was not truncated: %d", len(repository.input.Platform))
	}
	details := repository.input.Events[0].DetailsJSON
	if !strings.Contains(details, `"bytes_received":100`) ||
		!strings.Contains(details, `"sdp_bytes":123`) ||
		strings.Contains(details, "secret") ||
		strings.Contains(details, "1.2.3.4") {

		t.Fatalf("unexpected sanitized details: %s", details)
	}
}

func TestServiceBoundsOversizedClientDiagnosticDetails(t *testing.T) {
	repository := &repositoryStub{}
	service := NewService(repository)
	now := time.Unix(1_800_000_000, 0)
	service.now = func() time.Time { return now }

	_, err := service.Ingest(context.Background(), 10, BatchInput{
		SessionID: "session-1",
		Events: []EventInput{{
			ClientTimestampMS: now.UnixMilli(),
			Category:          "media",
			Name:              "large_stats",
			Level:             "info",
			DetailsJSON:       `{"reports":["` + strings.Repeat("x", MaxDetailsBytes*2) + `"]}`,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := repository.input.Events[0].DetailsJSON; len(got) > MaxDetailsBytes {
		t.Fatalf("sanitized details size = %d, want <= %d", len(got), MaxDetailsBytes)
	}
}

func TestServiceRejectsExpiredClientDiagnostic(t *testing.T) {
	repository := &repositoryStub{}
	service := NewService(repository)
	now := time.Unix(1_800_000_000, 0)
	service.now = func() time.Time { return now }

	_, err := service.Ingest(
		context.Background(),
		10,
		BatchInput{
			SessionID: "session-1",
			Events: []EventInput{{
				ClientTimestampMS: now.Add(-MaxClientEventAge - time.Second).UnixMilli(),
				Category:          "http",
				Name:              "request.complete",
				Level:             "info",
				DetailsJSON:       `{}`,
			}},
		},
	)
	if err != ErrInvalidEvent {
		t.Fatalf("error = %v, want %v", err, ErrInvalidEvent)
	}
}

func TestServiceRestrictsDiagnosticListingToOwner(t *testing.T) {
	repository := &repositoryStub{}
	service := NewService(repository)

	if _, err := service.List(
		context.Background(),
		10,
		ListFilter{},
	); err != ErrForbidden {

		t.Fatalf("non-owner error = %v, want %v", err, ErrForbidden)
	}

	repository.owner = true
	events, err := service.List(
		context.Background(),
		10,
		ListFilter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ID != 1 {
		t.Fatalf("unexpected events: %+v", events)
	}
}

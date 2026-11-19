package sqlite

import (
	"context"
	"testing"
	"time"

	"voxhold-backend/internal/diagnostics"
	"voxhold-backend/internal/testdb"
)

func TestRepositoryStoresListsAndPrunesDiagnostics(t *testing.T) {
	db := testdb.Open(t)

	repository := NewRepository(db)
	oldCreatedAt := time.Now().Add(-diagnostics.Retention - time.Minute).Unix()
	if _, err := db.Exec(`
		INSERT INTO client_diagnostic_events (
			created_at, client_timestamp_ms, user_id, session_id,
			client_version, platform, category, event_name, level, details_json
		) VALUES (?, ?, 1, 'old-session', '1.0.0', 'test', 'http', 'old', 'info', '{}')
	`, oldCreatedAt, oldCreatedAt*1000); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	accepted, err := repository.InsertBatch(
		context.Background(),
		1,
		diagnostics.BatchInput{
			SessionID:     "session-1",
			ClientVersion: "1.0.3",
			Platform:      "test-browser",
			Events: []diagnostics.EventInput{
				{ClientTimestampMS: now.UnixMilli(), Category: "http", Name: "request_completed", Level: "info", DetailsJSON: `{"status":200}`},
				{ClientTimestampMS: now.UnixMilli(), Category: "media", Name: "voice_rtp_stats", Level: "info", DetailsJSON: `{"bytes":100}`},
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if accepted != 2 {
		t.Fatalf("accepted = %d, want 2", accepted)
	}
	if err := repository.Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}

	var oldCount int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM client_diagnostic_events WHERE session_id = 'old-session'",
	).Scan(&oldCount); err != nil {
		t.Fatal(err)
	}
	if oldCount != 0 {
		t.Fatalf("expired diagnostics count = %d, want 0", oldCount)
	}

	events, err := repository.List(context.Background(), diagnostics.ListFilter{
		Since: now.Add(-time.Minute).Unix(),
		Limit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Username != "owner" || events[0].Name != "voice_rtp_stats" {
		t.Fatalf("unexpected diagnostics: %+v", events)
	}
	older, err := repository.List(context.Background(), diagnostics.ListFilter{
		Since:  now.Add(-time.Minute).Unix(),
		Limit:  1,
		Before: diagnostics.Cursor{CreatedAt: events[0].CreatedAt, ID: events[0].ID},
	})
	if err != nil || len(older) != 1 || older[0].Name != "request_completed" {
		t.Fatalf("unexpected second page: %+v, %v", older, err)
	}

	owner, err := repository.IsOwner(context.Background(), 1)
	if err != nil || !owner {
		t.Fatalf("owner = %t, error = %v", owner, err)
	}
}

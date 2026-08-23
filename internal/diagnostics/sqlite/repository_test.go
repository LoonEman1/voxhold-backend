package sqlite

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"voxhold-backend/internal/diagnostics"

	_ "modernc.org/sqlite"
)

func TestRepositoryStoresListsAndPrunesDiagnostics(t *testing.T) {
	db, err := sql.Open("sqlite", "file:diagnostics-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	const schema = `
	PRAGMA foreign_keys = ON;
	CREATE TABLE users (
		id INTEGER PRIMARY KEY,
		username TEXT NOT NULL,
		deleted_at INTEGER
	);
	CREATE TABLE server_members (
		server_id INTEGER NOT NULL,
		user_id INTEGER NOT NULL,
		role TEXT NOT NULL
	);
	CREATE TABLE client_diagnostic_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		created_at INTEGER NOT NULL DEFAULT (unixepoch()),
		client_timestamp_ms INTEGER NOT NULL,
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		session_id TEXT NOT NULL,
		client_version TEXT NOT NULL,
		platform TEXT NOT NULL,
		category TEXT NOT NULL,
		event_name TEXT NOT NULL,
		level TEXT NOT NULL,
		details_json TEXT NOT NULL
	);
	INSERT INTO users (id, username) VALUES (1, 'owner');
	INSERT INTO server_members (server_id, user_id, role) VALUES (10, 1, 'owner');
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}

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
		Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Username != "owner" {
		t.Fatalf("unexpected diagnostics: %+v", events)
	}

	owner, err := repository.IsOwner(context.Background(), 1)
	if err != nil || !owner {
		t.Fatalf("owner = %t, error = %v", owner, err)
	}
}

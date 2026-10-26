package sqlite

import (
	"context"
	"testing"
	"time"

	"voxhold-backend/internal/diagnostics"
	"voxhold-backend/internal/testdb"
)

func TestDiagnosticStorageBoundAndCountSurviveEvictionAndCleanup(t *testing.T) {
	db := testdb.Open(t)
	_, err := db.Exec(`WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x < ?)
	INSERT INTO client_diagnostic_events(created_at, client_timestamp_ms, user_id, session_id, client_version, platform, category, event_name, level, details_json)
	SELECT ?, 1, 2, 'old', 'test', 'browser', 'http', 'request', 'info', '{}' FROM n`, diagnostics.MaxStoredEvents, time.Now().Add(-25*time.Hour).Unix())
	if err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(db)
	batch := diagnostics.BatchInput{SessionID: "new", Events: make([]diagnostics.EventInput, diagnostics.MaxBatchEvents)}
	for index := range batch.Events {
		batch.Events[index] = diagnostics.EventInput{ClientTimestampMS: time.Now().UnixMilli(), Category: "media", Name: "sample", Level: "info", DetailsJSON: "{}"}
	}
	accepted, err := repo.InsertBatch(context.Background(), 1, batch)
	if err != nil || accepted != diagnostics.MaxBatchEvents {
		t.Fatal(accepted, err)
	}
	assertCount := func(want int) {
		t.Helper()
		var actual, tracked int
		if err := db.QueryRow("SELECT COUNT(*) FROM client_diagnostic_events").Scan(&actual); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow("SELECT event_count FROM client_diagnostic_storage WHERE id = 1").Scan(&tracked); err != nil {
			t.Fatal(err)
		}
		if actual != want || tracked != want {
			t.Fatal(actual, tracked, want)
		}
	}
	assertCount(diagnostics.MaxStoredEvents)
	if err := repo.Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertCount(diagnostics.MaxStoredEvents - 500)
	if _, err := db.Exec("DELETE FROM client_diagnostic_events WHERE user_id = 1"); err != nil {
		t.Fatal(err)
	}
	assertCount(diagnostics.MaxStoredEvents - 500 - diagnostics.MaxBatchEvents)
}

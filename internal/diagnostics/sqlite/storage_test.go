package sqlite

import (
	"context"
	"testing"
	"time"

	"voxhold-backend/internal/diagnostics"
	"voxhold-backend/internal/testdb"
)

func TestDiagnosticStorageEvictsOnlyTheOldestBytes(t *testing.T) {
	db := testdb.Open(t)
	now := time.Now().Unix()
	const eventBytes = 4096
	count := diagnostics.MaxStoredBytes / eventBytes
	_, err := db.Exec(`WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i < ?)
	INSERT INTO client_diagnostic_events(created_at, client_timestamp_ms, user_id, session_id, client_version, platform, category, event_name, level, details_json)
	SELECT ?, 1, 2, 'seed', '', '', 'media', 'sample', 'info', '"' || printf('%0*d', ?, 0) || '"' FROM n`,
		count, now-1, eventBytes-64-len("seedmediasampleinfo")-2)
	if err != nil {
		t.Fatal(err)
	}
	assertBytes := func(want int) {
		t.Helper()
		var actual, tracked int
		if err := db.QueryRow("SELECT COALESCE(SUM(byte_size), 0) FROM client_diagnostic_events").Scan(&actual); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow("SELECT total_bytes FROM client_diagnostic_storage WHERE id = 1").Scan(&tracked); err != nil {
			t.Fatal(err)
		}
		if actual != want || tracked != want {
			t.Fatalf("actual=%d tracked=%d want=%d", actual, tracked, want)
		}
	}
	assertBytes(count * eventBytes)
	repo := NewRepository(db)
	batch := diagnostics.BatchInput{SessionID: "new", Events: make([]diagnostics.EventInput, diagnostics.MaxBatchEvents)}
	for index := range batch.Events {
		batch.Events[index] = diagnostics.EventInput{ClientTimestampMS: now * 1000, Category: "media", Name: "sample", Level: "info", DetailsJSON: `{"value":"тест"}`}
	}
	accepted, err := repo.InsertBatch(context.Background(), 3, batch)
	if err != nil || accepted != len(batch.Events) {
		t.Fatal(accepted, err)
	}
	var oldest, remaining int
	if err := db.QueryRow("SELECT MIN(id), COUNT(*) FROM client_diagnostic_events WHERE user_id = 2").Scan(&oldest, &remaining); err != nil {
		t.Fatal(err)
	}
	newBytes := len(batch.Events) * (64 + len("newmediasampleinfo") + len(batch.Events[0].DetailsJSON))
	evicted := (count*eventBytes + newBytes - diagnostics.MaxStoredBytes + eventBytes - 1) / eventBytes
	if oldest != evicted+1 || remaining != count-evicted {
		t.Fatalf("oldest=%d remaining=%d expected %d evictions", oldest, remaining, evicted)
	}
	assertBytes(remaining*eventBytes + newBytes)
	if _, err := db.Exec("DELETE FROM users WHERE id = 3"); err != nil {
		t.Fatal(err)
	}
	assertBytes(remaining * eventBytes)
}

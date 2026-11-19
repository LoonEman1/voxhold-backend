package sqlite

import (
	"context"
	"testing"
	"time"
	"voxhold-backend/internal/diagnostics"
	"voxhold-backend/internal/testdb"
)

func TestDiagnosticsAcceptNewEventsAfterOldUserQuotaAndPruneExpiredBatches(t *testing.T) {
	db := testdb.Open(t)
	seed := func(count int, at int64) {
		t.Helper()
		_, err := db.Exec(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<?)
 INSERT INTO client_diagnostic_events(created_at,client_timestamp_ms,user_id,session_id,client_version,platform,category,event_name,level,details_json)
 SELECT ?,?*1000,1,'fixture','2.5.3','test','http','request_completed','info','{}' FROM n`, count, at, at)
		if err != nil {
			t.Fatal(err)
		}
	}
	seed(2000, time.Now().Unix())
	repository := NewRepository(db)
	accepted, err := repository.InsertBatch(context.Background(), 1, diagnostics.BatchInput{SessionID: "fixture", Events: []diagnostics.EventInput{{Category: "http", Name: "request_completed", Level: "info", DetailsJSON: "{}"}}})
	if err != nil || accepted != 1 {
		t.Fatalf("accepted: %d %v", accepted, err)
	}
	if _, err := db.Exec(`DELETE FROM client_diagnostic_events`); err != nil {
		t.Fatal(err)
	}
	seed(2100, time.Now().Add(-diagnostics.Retention-time.Minute).Unix())
	seed(1, time.Now().Unix())
	if err := repository.Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	var count, bytes, stored int
	if err := db.QueryRow(`SELECT COUNT(*), SUM(byte_size) FROM client_diagnostic_events`).Scan(&count, &bytes); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT total_bytes FROM client_diagnostic_storage WHERE id=1`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if count != 101 || stored != bytes {
		t.Fatalf("cleanup batch: rows=%d bytes=%d counter=%d", count, bytes, stored)
	}
	if err := repository.Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM client_diagnostic_events`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("retention: %d %v", count, err)
	}
}

package audit

import (
	"context"
	"testing"
	"voxhold-backend/internal/testdb"
)

func TestRecordResolvesNamesForChannelPermissionChanges(t *testing.T) {
	db := testdb.Open(t)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := Record(context.Background(), tx, 1, 1, "channel.permissions", "channel", 2, "", nil); err != nil {
		t.Fatal(err)
	}
	var name string
	if err := tx.QueryRow(`SELECT target_name FROM admin_audit WHERE action='channel.permissions'`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "Text" {
		t.Fatalf("want channel name, got %q", name)
	}
}

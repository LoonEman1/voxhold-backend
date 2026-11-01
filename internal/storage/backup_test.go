package storage_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"voxhold-backend/internal/storage"
	"voxhold-backend/internal/testdb"
)

func TestBackupCreatesVerifiedSnapshot(t *testing.T) {
	db := testdb.OpenFile(t)
	if _, err := db.Exec(`INSERT INTO users(id,username,password_hash) VALUES(70,'snap','h')`); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), ".backup", "voxhold.db")
	if err := storage.Backup(context.Background(), db, target); err != nil {
		t.Fatal(err)
	}
	// A second run replaces the earlier snapshot.
	if err := storage.Backup(context.Background(), db, target); err != nil {
		t.Fatal(err)
	}
	snapshot, err := sql.Open("sqlite", "file:"+filepath.ToSlash(target)+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	var name string
	if err = snapshot.QueryRow(`SELECT username FROM users WHERE id=70`).Scan(&name); err != nil || name != "snap" {
		t.Fatalf("name=%q err=%v", name, err)
	}
}

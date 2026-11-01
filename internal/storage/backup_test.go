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

func TestBackupRefusesToOverwriteTheLiveDatabase(t *testing.T) {
	db := testdb.OpenFile(t)
	var live string
	if err := db.QueryRow(`SELECT file FROM pragma_database_list WHERE name='main'`).Scan(&live); err != nil || live == "" {
		t.Fatalf("live=%q err=%v", live, err)
	}
	for _, target := range []string{live, live + "-wal"} {
		if err := storage.Backup(context.Background(), db, target); err == nil {
			t.Fatalf("snapshot into %s was accepted", target)
		}
	}
	var users int
	if err := db.QueryRow(`SELECT count(*) FROM users`).Scan(&users); err != nil || users == 0 {
		t.Fatalf("live database damaged: users=%d err=%v", users, err)
	}
}

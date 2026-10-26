// Package testdb builds an isolated database using the real migration chain.
package testdb

import (
	"database/sql"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
)

func Open(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if _, err = db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	_, file, _, _ := runtime.Caller(0)
	files, err := filepath.Glob(filepath.Join(filepath.Dir(file), "..", "..", "migrations", "*.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(string(data)); err != nil {
			t.Fatalf("migration %s: %v", file, err)
		}
	}
	_, err = db.Exec(`INSERT INTO users(id,username,password_hash) VALUES(1,'owner','hash'),(2,'member','hash'),(3,'outsider','hash');
 INSERT INTO servers(id,name,created_by) VALUES(1,'First',1);
 INSERT INTO server_members(server_id,user_id,role) VALUES(1,1,'owner'),(1,2,'member');
 INSERT INTO channels(id,server_id,name,kind,created_by) VALUES(1,1,'Voice','voice',1),(2,1,'Text','text',1),(4,1,'Voice2','voice',1);`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

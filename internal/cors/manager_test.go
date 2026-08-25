package cors

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	_ "modernc.org/sqlite"
)

func TestManagerReplaceUpdatesRuntimeSnapshot(t *testing.T) {
	db := openTestDB(t)
	manager, err := NewManager(context.Background(), db)
	if err != nil {
		t.Fatalf("create manager: %v", err)
	}

	origins, err := manager.Replace(context.Background(), 1, []string{
		"https://Client.Example.com/",
		"http://localhost:3000",
		"https://client.example.com",
	})
	if err != nil {
		t.Fatalf("replace origins: %v", err)
	}

	want := []string{"http://localhost:3000", "https://client.example.com"}
	assertOrigins(t, origins, want)
	assertOrigins(t, manager.ConfiguredOrigins(), want)

	reloaded, err := NewManager(context.Background(), db)
	if err != nil {
		t.Fatalf("reload manager: %v", err)
	}
	assertOrigins(t, reloaded.ConfiguredOrigins(), want)
}

func TestManagerOnlyAllowsOwnerToManageOrigins(t *testing.T) {
	db := openTestDB(t)
	manager, err := NewManager(context.Background(), db)
	if err != nil {
		t.Fatalf("create manager: %v", err)
	}

	if _, err := manager.Replace(
		context.Background(),
		2,
		[]string{"https://client.example.com"},
	); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member update error = %v, want %v", err, ErrForbidden)
	}
	if _, err := manager.List(context.Background(), 2); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member list error = %v, want %v", err, ErrForbidden)
	}
	if got := manager.ConfiguredOrigins(); len(got) != 0 {
		t.Fatalf("forbidden update changed origins: %v", got)
	}
}

func TestNormalizeOriginRejectsNonOrigins(t *testing.T) {
	invalid := []string{
		"*",
		"null",
		"ftp://example.com",
		"https://example.com/path",
		"https://example.com?query=yes",
		"https://user@example.com",
		"https://example.com:70000",
		"https://*.example.com",
		"https://under_score.example.com",
	}

	for _, value := range invalid {
		t.Run(value, func(t *testing.T) {
			if _, err := normalizeOrigin(value); err == nil {
				t.Fatalf("normalizeOrigin(%q) succeeded", value)
			}
		})
	}
}

func TestOriginPatternsAreExactAndSupportIPv6(t *testing.T) {
	manager := &Manager{}
	manager.store([]string{"https://[2001:db8::1]:8443"})

	patterns := manager.OriginPatterns()
	got := patterns[len(patterns)-1]
	want := `https://\[2001:db8::1]:8443`
	if got != want {
		t.Fatalf("IPv6 origin pattern = %q, want %q", got, want)
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	const schema = `
	PRAGMA foreign_keys = ON;
	CREATE TABLE users (
		id INTEGER PRIMARY KEY,
		username TEXT NOT NULL
	);
	CREATE TABLE servers (
		id INTEGER PRIMARY KEY,
		created_by INTEGER NOT NULL REFERENCES users(id)
	);
	CREATE TABLE server_members (
		server_id INTEGER NOT NULL REFERENCES servers(id),
		user_id INTEGER NOT NULL REFERENCES users(id),
		role TEXT NOT NULL,
		PRIMARY KEY (server_id, user_id)
	);
	CREATE TABLE cors_origins (
		origin TEXT PRIMARY KEY,
		created_by INTEGER NOT NULL REFERENCES users(id),
		created_at INTEGER NOT NULL DEFAULT (unixepoch())
	);
	INSERT INTO users (id, username) VALUES (1, 'owner'), (2, 'member');
	INSERT INTO servers (id, created_by) VALUES (1, 1);
	INSERT INTO server_members (server_id, user_id, role)
	VALUES (1, 1, 'owner'), (1, 2, 'member');
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	return db
}

func assertOrigins(t *testing.T, got []string, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("origins = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("origins = %v, want %v", got, want)
		}
	}
}

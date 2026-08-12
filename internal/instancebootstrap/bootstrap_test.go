package instancebootstrap_test

import (
	"context"
	"database/sql"
	"testing"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"

	"voxhold-backend/internal/instancebootstrap"
)

func TestEnsureCreatesExactlyOneInstance(t *testing.T) {
	db := openBootstrapDB(t)
	ctx := context.Background()

	result, err := instancebootstrap.Ensure(ctx, db, instancebootstrap.Config{
		Username:     "first_owner",
		InstanceName: "Friends",
	})
	if err != nil {
		t.Fatalf("bootstrap instance: %v", err)
	}
	if !result.Created || !result.PasswordGenerated || result.Password == "" {
		t.Fatalf("unexpected bootstrap result: %#v", result)
	}

	var username, passwordHash, instanceName, role string
	if err := db.QueryRow(`
		SELECT users.username, users.password_hash, servers.name, server_members.role
		FROM servers
		JOIN server_members ON server_members.server_id = servers.id
		JOIN users ON users.id = server_members.user_id
	`).Scan(&username, &passwordHash, &instanceName, &role); err != nil {
		t.Fatalf("read bootstrapped instance: %v", err)
	}
	if username != "first_owner" || instanceName != "Friends" || role != "owner" {
		t.Fatalf("unexpected bootstrap rows: user=%q instance=%q role=%q", username, instanceName, role)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(result.Password)); err != nil {
		t.Fatalf("generated password does not match stored hash: %v", err)
	}
	if err := instancebootstrap.Validate(ctx, db); err != nil {
		t.Fatalf("validate bootstrapped instance: %v", err)
	}

	second, err := instancebootstrap.Ensure(ctx, db, instancebootstrap.Config{
		Username:     "different",
		Password:     "different password",
		InstanceName: "Different",
	})
	if err != nil {
		t.Fatalf("repeat bootstrap: %v", err)
	}
	if second.Created {
		t.Fatal("repeat bootstrap created another instance")
	}
}

func openBootstrapDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open bootstrap database: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	const schema = `
	PRAGMA foreign_keys = ON;
	CREATE TABLE users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		deleted_at INTEGER
	);
	CREATE TABLE servers (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		created_by INTEGER NOT NULL REFERENCES users(id),
		created_at INTEGER NOT NULL DEFAULT (unixepoch())
	);
	CREATE TABLE server_members (
		server_id INTEGER NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		role TEXT NOT NULL,
		joined_at INTEGER NOT NULL DEFAULT (unixepoch()),
		PRIMARY KEY (server_id, user_id)
	);
	CREATE TABLE user_bans (
		user_id INTEGER PRIMARY KEY REFERENCES users(id),
		banned_by_user_id INTEGER NOT NULL REFERENCES users(id)
	);
	CREATE TRIGGER prevent_multiple_servers
	BEFORE INSERT ON servers
	WHEN EXISTS (SELECT 1 FROM servers)
	BEGIN
		SELECT RAISE(ABORT, 'this Voxhold instance already has a server');
	END;
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create bootstrap schema: %v", err)
	}
	return db
}

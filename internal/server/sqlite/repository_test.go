package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"voxhold-backend/internal/server"
	serversqlite "voxhold-backend/internal/server/sqlite"

	_ "modernc.org/sqlite"
)

func TestRepositorySingleInstanceLifecycle(t *testing.T) {
	db := openServerTestDB(t)
	repository := serversqlite.NewRepository(db)
	ctx := context.Background()

	instance, err := repository.GetInstance(ctx)
	if err != nil {
		t.Fatalf("get empty instance: %v", err)
	}
	if instance.ID != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("unexpected instance ID: %q", instance.ID)
	}
	if instance.Initialized || instance.Name != "" {
		t.Fatalf("empty instance is initialized: %#v", instance)
	}

	created, err := repository.Create(ctx, "Voxhold", 1)
	if err != nil {
		t.Fatalf("create instance server: %v", err)
	}
	if created.Name != "Voxhold" || created.CreatedBy != 1 {
		t.Fatalf("unexpected server: %#v", created)
	}

	instance, err = repository.GetInstance(ctx)
	if err != nil {
		t.Fatalf("get initialized instance: %v", err)
	}
	if !instance.Initialized || instance.Name != "Voxhold" {
		t.Fatalf("unexpected initialized instance: %#v", instance)
	}

	if _, err := repository.Create(ctx, "Second", 1); !errors.Is(err, server.ErrAlreadyExists) {
		t.Fatalf("second server error = %v, want %v", err, server.ErrAlreadyExists)
	}

	if err := repository.Delete(ctx, created.ID, 1); !errors.Is(err, server.ErrLastServerDelete) {
		t.Fatalf("delete last server error = %v, want %v", err, server.ErrLastServerDelete)
	}

	var membershipCount int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM server_members WHERE server_id = ? AND user_id = ? AND role = ?",
		created.ID,
		1,
		server.RoleOwner,
	).Scan(&membershipCount); err != nil {
		t.Fatalf("count owner memberships: %v", err)
	}
	if membershipCount != 1 {
		t.Fatalf("owner membership count = %d, want 1", membershipCount)
	}
}

func TestDatabaseRejectsSecondServer(t *testing.T) {
	db := openServerTestDB(t)

	if _, err := db.Exec(
		"INSERT INTO servers (name, created_by) VALUES (?, ?)",
		"Voxhold",
		1,
	); err != nil {
		t.Fatalf("insert first server: %v", err)
	}

	if _, err := db.Exec(
		"INSERT INTO servers (name, created_by) VALUES (?, ?)",
		"Second",
		1,
	); err == nil {
		t.Fatal("database allowed a second server")
	}
}

func TestBanMemberRevokesInstanceAccess(t *testing.T) {
	db := openServerTestDB(t)
	repository := serversqlite.NewRepository(db)
	ctx := context.Background()

	created, err := repository.Create(ctx, "Voxhold", 1)
	if err != nil {
		t.Fatalf("create instance server: %v", err)
	}
	memberID := createTestMember(t, db, created.ID, "member")
	if _, err := db.Exec(
		"INSERT INTO sessions (user_id, token_hash, expires_at) VALUES (?, randomblob(32), unixepoch() + 3600)",
		memberID,
	); err != nil {
		t.Fatalf("create member session: %v", err)
	}

	if err := repository.BanMember(ctx, created.ID, 1, memberID); err != nil {
		t.Fatalf("ban member: %v", err)
	}

	assertServerTestCount(t, db, "SELECT COUNT(*) FROM user_bans WHERE user_id = ?", memberID, 1)
	assertServerTestCount(t, db, "SELECT COUNT(*) FROM sessions WHERE user_id = ?", memberID, 0)
	assertServerTestCount(t, db, "SELECT COUNT(*) FROM server_members WHERE user_id = ?", memberID, 0)
}

func TestDeleteAccountAnonymizesMember(t *testing.T) {
	db := openServerTestDB(t)
	repository := serversqlite.NewRepository(db)
	ctx := context.Background()

	created, err := repository.Create(ctx, "Voxhold", 1)
	if err != nil {
		t.Fatalf("create instance server: %v", err)
	}
	memberID := createTestMember(t, db, created.ID, "departing")
	if _, err := db.Exec("INSERT INTO user_profiles (user_id, about) VALUES (?, 'private')", memberID); err != nil {
		t.Fatalf("create member profile: %v", err)
	}
	if _, err := db.Exec(
		"INSERT INTO sessions (user_id, token_hash, expires_at) VALUES (?, randomblob(32), unixepoch() + 3600)",
		memberID,
	); err != nil {
		t.Fatalf("create member session: %v", err)
	}

	serverID, err := repository.DeleteAccount(ctx, memberID)
	if err != nil {
		t.Fatalf("delete account: %v", err)
	}
	if serverID != created.ID {
		t.Fatalf("deleted account server ID = %d, want %d", serverID, created.ID)
	}

	var username string
	var deletedAt sql.NullInt64
	if err := db.QueryRow("SELECT username, deleted_at FROM users WHERE id = ?", memberID).Scan(&username, &deletedAt); err != nil {
		t.Fatalf("get deleted account: %v", err)
	}
	if username == "departing" || !deletedAt.Valid {
		t.Fatalf("account was not anonymized: username=%q deleted_at=%v", username, deletedAt)
	}
	assertServerTestCount(t, db, "SELECT COUNT(*) FROM user_profiles WHERE user_id = ?", memberID, 0)
	assertServerTestCount(t, db, "SELECT COUNT(*) FROM sessions WHERE user_id = ?", memberID, 0)
	assertServerTestCount(t, db, "SELECT COUNT(*) FROM server_members WHERE user_id = ?", memberID, 0)

	if _, err := db.Exec("INSERT INTO users (username, password_hash) VALUES ('departing', 'new hash')"); err != nil {
		t.Fatalf("reuse deleted username: %v", err)
	}

	if _, err := repository.DeleteAccount(ctx, 1); !errors.Is(err, server.ErrOwnerCannotLeave) {
		t.Fatalf("owner delete error = %v, want %v", err, server.ErrOwnerCannotLeave)
	}
}

func openServerTestDB(t *testing.T) *sql.DB {
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
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL DEFAULT 'hash',
		deleted_at INTEGER
	);
	CREATE TABLE sessions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		token_hash BLOB NOT NULL UNIQUE,
		expires_at INTEGER NOT NULL,
		created_at INTEGER NOT NULL DEFAULT (unixepoch())
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

	CREATE TABLE instance_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		instance_id TEXT NOT NULL UNIQUE CHECK (length(instance_id) = 32),
		created_at INTEGER NOT NULL DEFAULT (unixepoch())
	);
	CREATE TABLE user_profiles (
		user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
		about TEXT NOT NULL DEFAULT ''
	);
	CREATE TABLE channels (
		id INTEGER PRIMARY KEY,
		server_id INTEGER NOT NULL REFERENCES servers(id) ON DELETE CASCADE
	);
	CREATE TABLE channel_reads (
		channel_id INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		last_read_message_id INTEGER NOT NULL,
		PRIMARY KEY (channel_id, user_id)
	);
	CREATE TABLE server_invites (
		id INTEGER PRIMARY KEY,
		inviter_user_id INTEGER NOT NULL REFERENCES users(id),
		invitee_user_id INTEGER NOT NULL REFERENCES users(id)
	);
	CREATE TABLE server_invite_links (
		id INTEGER PRIMARY KEY,
		created_by INTEGER NOT NULL REFERENCES users(id)
	);
	CREATE TABLE user_bans (
		user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
		banned_by_user_id INTEGER NOT NULL REFERENCES users(id),
		created_at INTEGER NOT NULL DEFAULT (unixepoch())
	);

	INSERT INTO users (username) VALUES ('owner');
	INSERT INTO instance_settings (id, instance_id)
	VALUES (1, '0123456789abcdef0123456789abcdef');

	CREATE TRIGGER prevent_multiple_servers
	BEFORE INSERT ON servers
	WHEN EXISTS (SELECT 1 FROM servers)
	BEGIN
		SELECT RAISE(ABORT, 'this Voxhold instance already has a server');
	END;
	`

	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	return db
}

func createTestMember(t *testing.T, db *sql.DB, serverID int64, username string) int64 {
	t.Helper()

	result, err := db.Exec("INSERT INTO users (username) VALUES (?)", username)
	if err != nil {
		t.Fatalf("create test member: %v", err)
	}
	userID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("get test member ID: %v", err)
	}
	if _, err := db.Exec(
		"INSERT INTO server_members (server_id, user_id, role) VALUES (?, ?, 'member')",
		serverID,
		userID,
	); err != nil {
		t.Fatalf("add test membership: %v", err)
	}
	return userID
}

func assertServerTestCount(t *testing.T, db *sql.DB, query string, argument any, expected int) {
	t.Helper()

	var actual int
	if err := db.QueryRow(query, argument).Scan(&actual); err != nil {
		t.Fatalf("query count: %v", err)
	}
	if actual != expected {
		t.Fatalf("count = %d, want %d", actual, expected)
	}
}

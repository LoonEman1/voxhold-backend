package sqlite_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"voxhold-backend/internal/account"
	accountsqlite "voxhold-backend/internal/account/sqlite"
	invitesqlite "voxhold-backend/internal/invite/sqlite"
)

func TestRegistrationInviteIsConsumedAtomically(t *testing.T) {
	db := openInviteTestDB(t)
	ctx := context.Background()

	linkRepository := invitesqlite.NewRepository(db)
	registrationRepository := accountsqlite.NewInviteRegistrationRepository(db)
	tokenHash := sha256.Sum256([]byte("registration-link"))
	maxUses := 1

	createdLink, err := linkRepository.CreateLink(
		ctx,
		1,
		1,
		tokenHash[:],
		time.Now().Add(time.Hour).Unix(),
		&maxUses,
		true,
	)
	if err != nil {
		t.Fatalf("create invite link: %v", err)
	}
	if createdLink.ServerName != "Voxhold" || createdLink.CreatorUsername != "owner" {
		t.Fatalf("unexpected link details: %#v", createdLink)
	}

	if err := registrationRepository.ValidateRegistrationInvite(ctx, tokenHash[:]); err != nil {
		t.Fatalf("validate invite before use: %v", err)
	}

	sessionHash := sha256.Sum256([]byte("session"))
	user, serverID, member, err := registrationRepository.RegisterWithInvite(
		ctx,
		"new_user",
		"password hash",
		tokenHash[:],
		sessionHash[:],
		time.Now().Add(time.Hour).Unix(),
	)
	if err != nil {
		t.Fatalf("register with invite: %v", err)
	}
	if user.ID <= 1 || serverID != 1 || member.UserID != user.ID {
		t.Fatalf("unexpected registration result: user=%#v server=%d member=%#v", user, serverID, member)
	}

	assertCount(t, db, "SELECT use_count FROM server_invite_links WHERE id = ?", createdLink.ID, 1)
	assertCount(t, db, "SELECT COUNT(*) FROM users WHERE username = ?", "new_user", 1)
	assertCount(t, db, "SELECT COUNT(*) FROM server_members WHERE server_id = 1 AND user_id = ?", user.ID, 1)
	assertCount(t, db, "SELECT COUNT(*) FROM sessions WHERE user_id = ?", user.ID, 1)

	if err := registrationRepository.ValidateRegistrationInvite(ctx, tokenHash[:]); !errors.Is(err, account.ErrRegistrationInviteInvalid) {
		t.Fatalf("expected exhausted registration invite, got %v", err)
	}

	_, _, alreadyMember, err := linkRepository.AcceptLink(ctx, tokenHash[:], user.ID)
	if err != nil {
		t.Fatalf("accept exhausted link as existing member: %v", err)
	}
	if !alreadyMember {
		t.Fatal("expected idempotent acceptance for existing member")
	}
}

func TestFailedRegistrationDoesNotConsumeInvite(t *testing.T) {
	db := openInviteTestDB(t)
	ctx := context.Background()

	linkRepository := invitesqlite.NewRepository(db)
	registrationRepository := accountsqlite.NewInviteRegistrationRepository(db)
	tokenHash := sha256.Sum256([]byte("rollback-link"))
	sessionHash := sha256.Sum256([]byte("rollback-session"))
	maxUses := 2

	createdLink, err := linkRepository.CreateLink(
		ctx,
		1,
		1,
		tokenHash[:],
		time.Now().Add(time.Hour).Unix(),
		&maxUses,
		true,
	)
	if err != nil {
		t.Fatalf("create invite link: %v", err)
	}

	_, _, _, err = registrationRepository.RegisterWithInvite(
		ctx,
		"owner",
		"password hash",
		tokenHash[:],
		sessionHash[:],
		time.Now().Add(time.Hour).Unix(),
	)
	if !errors.Is(err, account.ErrUsernameTaken) {
		t.Fatalf("expected username conflict, got %v", err)
	}

	assertCount(t, db, "SELECT use_count FROM server_invite_links WHERE id = ?", createdLink.ID, 0)
	if err := registrationRepository.ValidateRegistrationInvite(ctx, tokenHash[:]); err != nil {
		t.Fatalf("invite must remain valid after rollback: %v", err)
	}
}

func TestExistingUserAcceptsRegisteredOnlyLink(t *testing.T) {
	db := openInviteTestDB(t)
	ctx := context.Background()

	if _, err := db.Exec(
		"INSERT INTO users (username, password_hash) VALUES (?, ?)",
		"existing_user",
		"hash",
	); err != nil {
		t.Fatalf("create existing user: %v", err)
	}

	linkRepository := invitesqlite.NewRepository(db)
	registrationRepository := accountsqlite.NewInviteRegistrationRepository(db)
	tokenHash := sha256.Sum256([]byte("registered-users-link"))
	maxUses := 1

	createdLink, err := linkRepository.CreateLink(
		ctx,
		1,
		1,
		tokenHash[:],
		time.Now().Add(7*24*time.Hour).Unix(),
		&maxUses,
		false,
	)
	if err != nil {
		t.Fatalf("create registered-only link: %v", err)
	}

	if err := registrationRepository.ValidateRegistrationInvite(ctx, tokenHash[:]); !errors.Is(err, account.ErrRegistrationInviteInvalid) {
		t.Fatalf("registered-only link allowed registration: %v", err)
	}

	serverID, member, alreadyMember, err := linkRepository.AcceptLink(ctx, tokenHash[:], 2)
	if err != nil {
		t.Fatalf("accept registered-only link: %v", err)
	}
	if serverID != 1 || member.UserID != 2 || alreadyMember {
		t.Fatalf("unexpected acceptance: server=%d member=%#v already=%v", serverID, member, alreadyMember)
	}
	assertCount(t, db, "SELECT use_count FROM server_invite_links WHERE id = ?", createdLink.ID, 1)

	_, _, alreadyMember, err = linkRepository.AcceptLink(ctx, tokenHash[:], 2)
	if err != nil || !alreadyMember {
		t.Fatalf("second acceptance must be idempotent: already=%v err=%v", alreadyMember, err)
	}
	assertCount(t, db, "SELECT use_count FROM server_invite_links WHERE id = ?", createdLink.ID, 1)
}

func openInviteTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", "file:invite-tests?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	const schema = `
	CREATE TABLE users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		created_at INTEGER NOT NULL DEFAULT (unixepoch())
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
	CREATE TABLE user_profiles (
		user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
		about TEXT NOT NULL DEFAULT '',
		country_code TEXT,
		last_seen_at INTEGER
	);
	CREATE TABLE server_invite_links (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		server_id INTEGER NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
		created_by INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		token_hash BLOB NOT NULL UNIQUE,
		expires_at INTEGER NOT NULL,
		max_uses INTEGER,
		use_count INTEGER NOT NULL DEFAULT 0,
		revoked_at INTEGER,
		created_at INTEGER NOT NULL DEFAULT (unixepoch()),
		allow_registration INTEGER NOT NULL DEFAULT 0
	);
	INSERT INTO users (username, password_hash) VALUES ('owner', 'hash');
	INSERT INTO servers (name, created_by) VALUES ('Voxhold', 1);
	INSERT INTO server_members (server_id, user_id, role) VALUES (1, 1, 'owner');
	`

	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	return db
}

func assertCount(t *testing.T, db *sql.DB, query string, argument any, expected int) {
	t.Helper()

	var actual int
	if err := db.QueryRow(query, argument).Scan(&actual); err != nil {
		t.Fatalf("query count: %v", err)
	}
	if actual != expected {
		t.Fatalf("expected %d, got %d", expected, actual)
	}
}

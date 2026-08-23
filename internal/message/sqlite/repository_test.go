package sqlite_test

import (
	"context"
	"database/sql"
	"testing"

	"voxhold-backend/internal/message"
	messagesqlite "voxhold-backend/internal/message/sqlite"

	_ "modernc.org/sqlite"
)

func TestPinReturnsResourceAndIsIdempotent(t *testing.T) {
	db := openMessageTestDB(t)
	repository := messagesqlite.NewRepository(db)
	ctx := context.Background()

	createdPin, created, err := repository.Pin(ctx, 1, 1, 1, 1)
	if err != nil {
		t.Fatalf("pin message: %v", err)
	}
	if !created {
		t.Fatal("first pin was not reported as created")
	}
	assertPinnedMessage(t, createdPin)

	existingPin, created, err := repository.Pin(ctx, 1, 1, 1, 1)
	if err != nil {
		t.Fatalf("pin existing message: %v", err)
	}
	if created {
		t.Fatal("idempotent pin was reported as created")
	}
	assertPinnedMessage(t, existingPin)
	if existingPin.PinnedAt != createdPin.PinnedAt {
		t.Fatalf(
			"existing pinned_at = %d, want %d",
			existingPin.PinnedAt,
			createdPin.PinnedAt,
		)
	}
}

func assertPinnedMessage(t *testing.T, value message.PinnedMessage) {
	t.Helper()

	if value.Message.ID != 1 ||
		value.Message.ChannelID != 1 ||
		value.Message.Author.UserID != 2 ||
		value.Message.Author.Username != "author" ||
		value.Message.Content != "important" ||
		value.Message.CreatedAt != 100 ||
		value.PinnedBy.UserID != 1 ||
		value.PinnedBy.Username != "owner" ||
		value.PinnedAt <= 0 {

		t.Fatalf("unexpected pinned message: %#v", value)
	}
}

func openMessageTestDB(t *testing.T) *sql.DB {
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
		username TEXT NOT NULL UNIQUE
	);
	CREATE TABLE servers (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		created_by INTEGER NOT NULL REFERENCES users(id)
	);
	CREATE TABLE server_members (
		server_id INTEGER NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		role TEXT NOT NULL,
		PRIMARY KEY (server_id, user_id)
	);
	CREATE TABLE channels (
		id INTEGER PRIMARY KEY,
		server_id INTEGER NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
		kind TEXT NOT NULL
	);
	CREATE TABLE messages (
		id INTEGER PRIMARY KEY,
		channel_id INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
		author_user_id INTEGER NOT NULL REFERENCES users(id),
		content TEXT NOT NULL,
		created_at INTEGER NOT NULL DEFAULT (unixepoch()),
		edited_at INTEGER
	);
	CREATE TABLE message_pins (
		message_id INTEGER PRIMARY KEY REFERENCES messages(id) ON DELETE CASCADE,
		pinned_by_user_id INTEGER NOT NULL REFERENCES users(id),
		pinned_at INTEGER NOT NULL DEFAULT (unixepoch())
	);

	INSERT INTO users (id, username) VALUES (1, 'owner'), (2, 'author');
	INSERT INTO servers (id, name, created_by) VALUES (1, 'Voxhold', 1);
	INSERT INTO server_members (server_id, user_id, role) VALUES (1, 1, 'owner');
	INSERT INTO channels (id, server_id, kind) VALUES (1, 1, 'text');
	INSERT INTO messages (id, channel_id, author_user_id, content, created_at)
	VALUES (1, 1, 2, 'important', 100);
	`

	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	return db
}

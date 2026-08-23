package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"voxhold-backend/internal/channel"
	channelsqlite "voxhold-backend/internal/channel/sqlite"

	_ "modernc.org/sqlite"
)

func TestListByServerIDIncludesLastMessageID(t *testing.T) {
	db := openChannelTestDB(t)
	repository := channelsqlite.NewRepository(db)

	channels, err := repository.ListByServerID(
		context.Background(),
		1,
		1,
	)
	if err != nil {
		t.Fatalf("list channels: %v", err)
	}

	if len(channels) != 3 {
		t.Fatalf("channel count = %d, want 3", len(channels))
	}

	wantLastMessageIDs := []int64{105, 0, 0}
	for index, value := range channels {
		if value.LastMessageID != wantLastMessageIDs[index] {
			t.Fatalf(
				"channel %d last message ID = %d, want %d",
				value.ID,
				value.LastMessageID,
				wantLastMessageIDs[index],
			)
		}
	}
}

func TestListByServerIDStillRequiresMembership(t *testing.T) {
	db := openChannelTestDB(t)
	repository := channelsqlite.NewRepository(db)

	_, err := repository.ListByServerID(
		context.Background(),
		1,
		2,
	)
	if !errors.Is(err, channel.ErrForbidden) {
		t.Fatalf("list channels error = %v, want %v", err, channel.ErrForbidden)
	}
}

func openChannelTestDB(t *testing.T) *sql.DB {
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
		name TEXT NOT NULL,
		kind TEXT NOT NULL,
		position INTEGER NOT NULL,
		created_by INTEGER NOT NULL REFERENCES users(id),
		created_at INTEGER NOT NULL
	);
	CREATE TABLE messages (
		id INTEGER PRIMARY KEY,
		channel_id INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
		author_user_id INTEGER NOT NULL REFERENCES users(id),
		content TEXT NOT NULL
	);
	CREATE INDEX idx_messages_channel_id_id
		ON messages(channel_id, id DESC);

	INSERT INTO users (id, username) VALUES (1, 'owner'), (2, 'outsider');
	INSERT INTO servers (id, name, created_by) VALUES (1, 'Voxhold', 1);
	INSERT INTO server_members (server_id, user_id, role) VALUES (1, 1, 'owner');
	INSERT INTO channels (
		id,
		server_id,
		name,
		kind,
		position,
		created_by,
		created_at
	) VALUES
		(10, 1, 'general', 'text', 0, 1, 100),
		(11, 1, 'empty', 'text', 1, 1, 101),
		(12, 1, 'voice', 'voice', 2, 1, 102);
	INSERT INTO messages (id, channel_id, author_user_id, content) VALUES
		(100, 10, 1, 'first'),
		(105, 10, 1, 'latest');
	`

	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	return db
}

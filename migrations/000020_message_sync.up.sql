-- A durable channel cursor survives journal cleanup and message deletion.
CREATE TABLE message_sync_state (
    channel_id INTEGER PRIMARY KEY REFERENCES channels(id) ON DELETE CASCADE,
    cursor INTEGER NOT NULL DEFAULT 0,
    floor INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE message_changes (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_id INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    message_id INTEGER NOT NULL,
    created_at INTEGER NOT NULL DEFAULT (unixepoch())
);
CREATE INDEX idx_message_changes_channel_sequence ON message_changes(channel_id, sequence);
CREATE INDEX idx_message_changes_created ON message_changes(created_at, sequence);

CREATE TRIGGER messages_sync_insert AFTER INSERT ON messages BEGIN
    INSERT INTO message_changes(channel_id, message_id) VALUES (NEW.channel_id, NEW.id);
    INSERT INTO message_sync_state(channel_id, cursor) VALUES (NEW.channel_id, last_insert_rowid())
    ON CONFLICT(channel_id) DO UPDATE SET cursor = excluded.cursor;
END;
CREATE TRIGGER messages_sync_update AFTER UPDATE OF content, revision ON messages BEGIN
    INSERT INTO message_changes(channel_id, message_id) VALUES (NEW.channel_id, NEW.id);
    INSERT INTO message_sync_state(channel_id, cursor) VALUES (NEW.channel_id, last_insert_rowid())
    ON CONFLICT(channel_id) DO UPDATE SET cursor = excluded.cursor;
END;
CREATE TRIGGER messages_sync_delete BEFORE DELETE ON messages BEGIN
    INSERT INTO message_changes(channel_id, message_id)
    SELECT OLD.channel_id, OLD.id WHERE EXISTS(SELECT 1 FROM channels WHERE id = OLD.channel_id);
    INSERT INTO message_sync_state(channel_id, cursor)
    SELECT OLD.channel_id, last_insert_rowid() WHERE EXISTS(SELECT 1 FROM channels WHERE id = OLD.channel_id)
    ON CONFLICT(channel_id) DO UPDATE SET cursor = excluded.cursor;
END;

-- Keep the key after deleting a message: retrying an old send must not recreate it.
CREATE TABLE message_send_keys (
    channel_id INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    author_user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    client_message_id TEXT NOT NULL CHECK(length(client_message_id) BETWEEN 1 AND 64),
    content_hash TEXT NOT NULL CHECK(length(content_hash) = 64),
    message_id INTEGER REFERENCES messages(id) ON DELETE SET NULL,
    PRIMARY KEY(channel_id, author_user_id, client_message_id)
);

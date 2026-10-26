CREATE TABLE user_avatars (
    user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    content BLOB NOT NULL CHECK (length(content) <= 131072),
    etag TEXT NOT NULL,
    updated_at INTEGER NOT NULL DEFAULT (unixepoch())
);

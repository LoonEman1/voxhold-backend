ALTER TABLE users
ADD COLUMN deleted_at INTEGER
    CHECK (deleted_at IS NULL OR deleted_at >= 0);

CREATE TABLE user_bans (
    user_id INTEGER PRIMARY KEY,

    banned_by_user_id INTEGER NOT NULL,

    created_at INTEGER NOT NULL
        DEFAULT (unixepoch()),

    FOREIGN KEY (user_id)
        REFERENCES users(id)
        ON DELETE CASCADE,

    FOREIGN KEY (banned_by_user_id)
        REFERENCES users(id)
        ON DELETE RESTRICT,

    CHECK (user_id <> banned_by_user_id)
);

CREATE INDEX idx_user_bans_banned_by
    ON user_bans(banned_by_user_id);

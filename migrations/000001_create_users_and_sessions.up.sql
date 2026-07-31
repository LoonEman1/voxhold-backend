CREATE TABLE users(
    id INTEGER PRIMARY KEY AUTOINCREMENT,

    username TEXT NOT NULL UNIQUE
        CHECK (length(username) BETWEEN 3 AND 32),

    password_hash TEXT NOT NULL,

    created_at INTEGER NOT NULL
        DEFAULT (unixepoch())
);


CREATE TABLE sessions(
    id INTEGER PRIMARY KEY AUTOINCREMENT,

    user_id INTEGER NOT NULL,

    token_hash BLOB NOT NULL UNIQUE
        CHECK(length(token_hash) = 32),

    expires_at INTEGER NOT NULL,

    created_at INTEGER NOT NULL
        DEFAULT (unixepoch()),

    FOREIGN KEY (user_id)
        REFERENCES users(id)
        ON DELETE CASCADE

);

CREATE INDEX idx_sessions_user_id
    ON sessions(user_id);

CREATE INDEX idx_sessions_expires_at
    ON sessions(expires_at);
CREATE TABLE messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,

    channel_id INTEGER NOT NULL,

    author_user_id INTEGER NOT NULL,

    content TEXT NOT NULL
        CHECK (length(trim(content)) BETWEEN 1 AND 5120),
    
    created_at INTEGER NOT NULL
        DEFAULT (unixepoch()),

    edited_at INTEGER,


    FOREIGN KEY (channel_id)
        REFERENCES channels(id)
        ON DELETE CASCADE,

    FOREIGN KEY (author_user_id)
        REFERENCES users(id)
        ON DELETE RESTRICT
);

CREATE INDEX idx_messages_channel_id_id
    ON messages(channel_id, id DESC);
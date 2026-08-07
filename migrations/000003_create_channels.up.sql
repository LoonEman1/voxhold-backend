CREATE TABLE channels (
    id INTEGER PRIMARY KEY AUTOINCREMENT,

    server_id INTEGER NOT NULL,

    name TEXT NOT NULL
        CHECK (length(trim(name)) BETWEEN 1 AND 64),

    kind TEXT NOT NULL
        CHECK (kind IN ('text', 'voice')),

    position INTEGER NOT NULL DEFAULT 0
        CHECK (position >= 0),
    
    created_by INTEGER NOT NULL,

    created_at INTEGER NOT NULL
        DEFAULT (unixepoch()),

    UNIQUE(server_id, name),

    FOREIGN KEY (server_id)
        REFERENCES servers(id)
        ON DELETE CASCADE,

    FOREIGN KEY (created_by)
        REFERENCES users(id)
        ON DELETE RESTRICT
);

CREATE INDEX idx_channels_server_position
    ON channels(server_id, position);

CREATE INDEX idx_channels_created_by
    ON channels(created_by);

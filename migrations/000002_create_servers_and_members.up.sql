CREATE TABLE servers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,

    name TEXT NOT NULL
        CHECK (length(trim(name)) BETWEEN 1 AND 64),
    
    created_by INTEGER NOT NULL,

    created_at INTEGER NOT NULL
        DEFAULT (unixepoch()),
    
    FOREIGN KEY (created_by)
        REFERENCES users(id)
        ON DELETE RESTRICT
);

CREATE TABLE server_members (
    server_id INTEGER NOT NULL,

    user_id INTEGER NOT NULL,

    role TEXT NOT NULL DEFAULT 'member'
        CHECK (role IN ('owner', 'admin', 'member')),

    joined_at INTEGER NOT NULL
        DEFAULT (unixepoch()),

    PRIMARY KEY (server_id, user_id),

    FOREIGN KEY (server_id)
        REFERENCES servers(id)
        ON DELETE CASCADE,

    FOREIGN KEY (user_id)
        REFERENCES users(id)
        ON DELETE CASCADE
);


CREATE INDEX idx_servers_created_by
    ON servers(created_by);

CREATE INDEX idx_members_user_id
    ON server_members(user_id);
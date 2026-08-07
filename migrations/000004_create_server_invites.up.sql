CREATE TABLE server_invites (
    id INTEGER PRIMARY KEY AUTOINCREMENT,

    server_id INTEGER NOT NULL,

    inviter_user_id INTEGER NOT NULL,

    invitee_user_id INTEGER NOT NULL,

    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (
            status IN (
                'pending',
                'accepted',
                'declined',
                'canceled',
                'expired'
            )
        ),

    expires_at INTEGER NOT NULL,

    responded_at INTEGER,

    created_at INTEGER NOT NULL
        DEFAULT (unixepoch()),

    CHECK (inviter_user_id <> invitee_user_id),

    FOREIGN KEY (server_id)
        REFERENCES servers(id)
        ON DELETE CASCADE,

    FOREIGN KEY (inviter_user_id)
        REFERENCES users(id)
        ON DELETE CASCADE,

    FOREIGN KEY (invitee_user_id)
        REFERENCES users(id)
        ON DELETE CASCADE
);

CREATE UNIQUE INDEX idx_server_invites_pending
    ON server_invites(server_id, invitee_user_id)
    WHERE status = 'pending';

CREATE INDEX idx_server_invites_invitee_status
    ON server_invites(invitee_user_id, status, created_at);

CREATE INDEX idx_server_invites_inviter
    ON server_invites(inviter_user_id);

CREATE INDEX idx_server_invites_server
    ON server_invites(server_id);


CREATE TABLE server_invite_links (
    id INTEGER PRIMARY KEY AUTOINCREMENT,

    server_id INTEGER NOT NULL,

    created_by INTEGER NOT NULL,

    token_hash BLOB NOT NULL UNIQUE
        CHECK (length(token_hash) = 32),

    expires_at INTEGER NOT NULL,

    max_uses INTEGER
        CHECK (max_uses IS NULL OR max_uses > 0),

    use_count INTEGER NOT NULL DEFAULT 0
        CHECK (use_count >= 0),

    revoked_at INTEGER,

    created_at INTEGER NOT NULL
        DEFAULT (unixepoch()),

    CHECK (
        max_uses IS NULL
        OR use_count <= max_uses
    ),

    FOREIGN KEY (server_id)
        REFERENCES servers(id)
        ON DELETE CASCADE,

    FOREIGN KEY (created_by)
        REFERENCES users(id)
        ON DELETE CASCADE
);

CREATE INDEX idx_server_invite_links_server
    ON server_invite_links(server_id);

CREATE INDEX idx_server_invite_links_expires
    ON server_invite_links(expires_at);
CREATE TABLE client_diagnostic_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at INTEGER NOT NULL DEFAULT (unixepoch()),
    client_timestamp_ms INTEGER NOT NULL,
    user_id INTEGER NOT NULL,
    session_id TEXT NOT NULL
        CHECK (length(session_id) BETWEEN 1 AND 64),
    client_version TEXT NOT NULL
        CHECK (length(client_version) <= 32),
    platform TEXT NOT NULL
        CHECK (length(platform) <= 200),
    category TEXT NOT NULL
        CHECK (length(category) BETWEEN 1 AND 32),
    event_name TEXT NOT NULL
        CHECK (length(event_name) BETWEEN 1 AND 80),
    level TEXT NOT NULL
        CHECK (level IN ('debug', 'info', 'warn', 'error')),
    details_json TEXT NOT NULL DEFAULT '{}'
        CHECK (json_valid(details_json)),

    FOREIGN KEY (user_id)
        REFERENCES users(id)
        ON DELETE CASCADE
);

CREATE INDEX idx_client_diagnostics_created_at
    ON client_diagnostic_events(created_at, id);

CREATE INDEX idx_client_diagnostics_session
    ON client_diagnostic_events(session_id, created_at, id);

CREATE INDEX idx_client_diagnostics_user
    ON client_diagnostic_events(user_id, created_at, id);

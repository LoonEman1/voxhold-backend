CREATE TABLE cors_origins (
    origin TEXT PRIMARY KEY
        CHECK (length(origin) BETWEEN 1 AND 512),

    created_by INTEGER NOT NULL,

    created_at INTEGER NOT NULL
        DEFAULT (unixepoch()),

    FOREIGN KEY (created_by)
        REFERENCES users(id)
        ON DELETE RESTRICT
);

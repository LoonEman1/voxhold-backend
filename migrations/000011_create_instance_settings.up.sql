CREATE TABLE instance_settings (
    id INTEGER PRIMARY KEY
        CHECK (id = 1),

    instance_id TEXT NOT NULL UNIQUE
        CHECK (length(instance_id) = 32),

    created_at INTEGER NOT NULL
        DEFAULT (unixepoch())
);

INSERT INTO instance_settings (id, instance_id)
VALUES (1, lower(hex(randomblob(16))));

CREATE TRIGGER prevent_multiple_servers
BEFORE INSERT ON servers
WHEN EXISTS (
    SELECT 1
    FROM servers
)
BEGIN
    SELECT RAISE(
        ABORT,
        'this Voxhold instance already has a server'
    );
END;

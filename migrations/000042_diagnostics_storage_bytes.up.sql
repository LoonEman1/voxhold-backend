DROP TRIGGER diagnostic_storage_insert;
DROP TRIGGER diagnostic_storage_delete;

-- UTF-8 payload plus a fixed allowance for numeric fields and record metadata.
ALTER TABLE client_diagnostic_events ADD COLUMN byte_size INTEGER GENERATED ALWAYS AS (
    64 + length(CAST(session_id AS BLOB)) + length(CAST(client_version AS BLOB))
       + length(CAST(platform AS BLOB)) + length(CAST(category AS BLOB))
       + length(CAST(event_name AS BLOB)) + length(CAST(level AS BLOB))
       + length(CAST(details_json AS BLOB))
) VIRTUAL;

DELETE FROM client_diagnostic_events WHERE created_at < unixepoch() - 86400;
DELETE FROM client_diagnostic_events WHERE id IN (
    SELECT id FROM (
        SELECT id, SUM(byte_size) OVER (ORDER BY created_at DESC, id DESC ROWS UNBOUNDED PRECEDING) AS retained_bytes
        FROM client_diagnostic_events
    ) WHERE retained_bytes > 100000000
);

ALTER TABLE client_diagnostic_storage DROP COLUMN event_count;
ALTER TABLE client_diagnostic_storage ADD COLUMN total_bytes INTEGER NOT NULL DEFAULT 0 CHECK(total_bytes >= 0);
UPDATE client_diagnostic_storage SET total_bytes = (
    SELECT COALESCE(SUM(byte_size), 0) FROM client_diagnostic_events
) WHERE id = 1;

CREATE TRIGGER diagnostic_storage_insert AFTER INSERT ON client_diagnostic_events BEGIN
    UPDATE client_diagnostic_storage SET total_bytes = total_bytes + NEW.byte_size WHERE id = 1;
END;
CREATE TRIGGER diagnostic_storage_delete AFTER DELETE ON client_diagnostic_events BEGIN
    UPDATE client_diagnostic_storage SET total_bytes = total_bytes - OLD.byte_size WHERE id = 1;
END;
CREATE TRIGGER diagnostic_storage_update AFTER UPDATE ON client_diagnostic_events BEGIN
    UPDATE client_diagnostic_storage SET total_bytes = total_bytes + NEW.byte_size - OLD.byte_size WHERE id = 1;
END;

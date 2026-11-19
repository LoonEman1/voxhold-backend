DROP TRIGGER diagnostic_storage_update;
DROP TRIGGER diagnostic_storage_insert;
DROP TRIGGER diagnostic_storage_delete;

ALTER TABLE client_diagnostic_events DROP COLUMN byte_size;
ALTER TABLE client_diagnostic_storage DROP COLUMN total_bytes;
ALTER TABLE client_diagnostic_storage ADD COLUMN event_count INTEGER NOT NULL DEFAULT 0 CHECK(event_count >= 0);
UPDATE client_diagnostic_storage SET event_count = (
    SELECT COUNT(*) FROM client_diagnostic_events
) WHERE id = 1;

CREATE TRIGGER diagnostic_storage_insert AFTER INSERT ON client_diagnostic_events BEGIN
    UPDATE client_diagnostic_storage SET event_count = event_count + 1 WHERE id = 1;
END;
CREATE TRIGGER diagnostic_storage_delete AFTER DELETE ON client_diagnostic_events BEGIN
    UPDATE client_diagnostic_storage SET event_count = event_count - 1 WHERE id = 1;
END;

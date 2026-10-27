CREATE TABLE client_diagnostic_storage (
    id INTEGER PRIMARY KEY CHECK(id = 1),
    event_count INTEGER NOT NULL CHECK(event_count >= 0)
);
INSERT INTO client_diagnostic_storage(id, event_count) SELECT 1, COUNT(*) FROM client_diagnostic_events;
CREATE TRIGGER diagnostic_storage_insert AFTER INSERT ON client_diagnostic_events BEGIN
    UPDATE client_diagnostic_storage SET event_count = event_count + 1 WHERE id = 1;
END;
CREATE TRIGGER diagnostic_storage_delete AFTER DELETE ON client_diagnostic_events BEGIN
    UPDATE client_diagnostic_storage SET event_count = event_count - 1 WHERE id = 1;
END;

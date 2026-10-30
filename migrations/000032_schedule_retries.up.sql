ALTER TABLE scheduled_messages ADD COLUMN attempts INTEGER NOT NULL DEFAULT 0 CHECK(attempts >= 0);
ALTER TABLE scheduled_messages ADD COLUMN next_attempt_at INTEGER NOT NULL DEFAULT 0;
CREATE INDEX scheduled_messages_retry ON scheduled_messages(status,next_attempt_at,send_at,id);

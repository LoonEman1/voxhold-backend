DROP INDEX scheduled_messages_retry;
ALTER TABLE scheduled_messages DROP COLUMN next_attempt_at;
ALTER TABLE scheduled_messages DROP COLUMN attempts;

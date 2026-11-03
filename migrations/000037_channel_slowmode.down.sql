DROP INDEX IF EXISTS idx_messages_channel_author;
ALTER TABLE channels DROP COLUMN slowmode_seconds;

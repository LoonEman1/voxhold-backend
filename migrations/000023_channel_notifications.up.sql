CREATE TABLE channel_notification_settings (
    channel_id INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    server_id INTEGER NOT NULL,
    user_id INTEGER NOT NULL,
    mode TEXT NOT NULL CHECK (mode IN ('all', 'mentions', 'none')),
    PRIMARY KEY (channel_id, user_id),
    FOREIGN KEY (server_id, user_id) REFERENCES server_members(server_id, user_id) ON DELETE CASCADE
);
CREATE INDEX idx_channel_notification_settings_member ON channel_notification_settings(server_id, user_id);
CREATE TRIGGER channel_notification_scope BEFORE INSERT ON channel_notification_settings
WHEN NOT EXISTS (SELECT 1 FROM channels WHERE id=NEW.channel_id AND server_id=NEW.server_id AND kind='text')
BEGIN SELECT RAISE(ABORT, 'text channel notification scope mismatch'); END;

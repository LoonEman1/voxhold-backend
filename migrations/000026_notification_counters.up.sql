-- Only notifications created after this migration are counted; no old alerts
-- are replayed. Read cursors clear counts across devices. Message/member/channel
-- deletion cascades remove stale entries.
CREATE TABLE channel_notifications (
    channel_id INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    server_id INTEGER NOT NULL,
    user_id INTEGER NOT NULL,
    message_id INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    PRIMARY KEY(channel_id,user_id,message_id),
    FOREIGN KEY(server_id,user_id) REFERENCES server_members(server_id,user_id) ON DELETE CASCADE
);
CREATE INDEX idx_channel_notifications_member ON channel_notifications(server_id,user_id);
CREATE TRIGGER channel_notifications_scope BEFORE INSERT ON channel_notifications
WHEN NOT EXISTS (
    SELECT 1 FROM messages m JOIN channels c ON c.id=m.channel_id
    WHERE m.id=NEW.message_id AND c.id=NEW.channel_id AND c.server_id=NEW.server_id
)
BEGIN SELECT RAISE(ABORT,'notification channel scope mismatch'); END;
CREATE TRIGGER channel_notification_read_cleanup AFTER INSERT ON channel_reads
BEGIN DELETE FROM channel_notifications WHERE channel_id=NEW.channel_id AND user_id=NEW.user_id AND message_id<=NEW.last_read_message_id; END;
CREATE TRIGGER channel_notification_read_update AFTER UPDATE OF last_read_message_id ON channel_reads
BEGIN DELETE FROM channel_notifications WHERE channel_id=NEW.channel_id AND user_id=NEW.user_id AND message_id<=NEW.last_read_message_id; END;

-- Keep the existing text storage kind and foreign keys intact. The API exposes
-- is_news=1 as kind=news; all existing ACLs, search and sync continue to apply.
ALTER TABLE channels ADD COLUMN is_news INTEGER NOT NULL DEFAULT 0 CHECK(is_news IN (0,1) AND (is_news=0 OR kind='text'));
ALTER TABLE channels ADD COLUMN comments_enabled INTEGER NOT NULL DEFAULT 0 CHECK(comments_enabled IN (0,1) AND (comments_enabled=0 OR is_news=1));

CREATE TABLE channel_publishers (
    channel_id INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    server_id INTEGER NOT NULL,
    user_id INTEGER NOT NULL,
    PRIMARY KEY(channel_id,user_id),
    FOREIGN KEY(server_id,user_id) REFERENCES server_members(server_id,user_id) ON DELETE CASCADE
);
CREATE INDEX idx_channel_publishers_member ON channel_publishers(server_id,user_id);
CREATE TRIGGER channel_publisher_scope BEFORE INSERT ON channel_publishers
WHEN NOT EXISTS(SELECT 1 FROM channels WHERE id=NEW.channel_id AND server_id=NEW.server_id AND is_news=1)
BEGIN SELECT RAISE(ABORT,'news publisher scope mismatch'); END;

ALTER TABLE messages ADD COLUMN thread_root_id INTEGER REFERENCES messages(id) ON DELETE CASCADE;
CREATE INDEX idx_messages_thread ON messages(thread_root_id,id);
CREATE TRIGGER message_thread_scope BEFORE INSERT ON messages
WHEN NEW.thread_root_id IS NOT NULL AND NOT EXISTS(
    SELECT 1 FROM messages m JOIN channels c ON c.id=m.channel_id
    WHERE m.id=NEW.thread_root_id AND m.channel_id=NEW.channel_id
      AND m.thread_root_id IS NULL AND c.is_news=1
)
BEGIN SELECT RAISE(ABORT,'comment must belong to a news post'); END;
-- Updating the root revision makes comment counts part of durable message sync.
CREATE TRIGGER message_comment_insert AFTER INSERT ON messages
WHEN NEW.thread_root_id IS NOT NULL
BEGIN UPDATE messages SET revision=revision+1 WHERE id=NEW.thread_root_id; END;
CREATE TRIGGER message_comment_delete AFTER DELETE ON messages
WHEN OLD.thread_root_id IS NOT NULL
BEGIN UPDATE messages SET revision=revision+1 WHERE id=OLD.thread_root_id; END;

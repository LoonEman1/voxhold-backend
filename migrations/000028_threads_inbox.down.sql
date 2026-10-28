DROP INDEX inbox_user;
ALTER TABLE channel_notifications DROP COLUMN read_at;
DROP TABLE thread_subscriptions;
DROP TRIGGER message_thread_scope;
CREATE TRIGGER message_thread_scope BEFORE INSERT ON messages
WHEN NEW.thread_root_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM messages m JOIN channels c ON c.id=m.channel_id WHERE m.id=NEW.thread_root_id AND m.channel_id=NEW.channel_id AND m.thread_root_id IS NULL AND c.is_news=1)
BEGIN SELECT RAISE(ABORT,'comment must belong to a news post'); END;

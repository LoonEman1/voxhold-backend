DROP TRIGGER message_thread_scope;
CREATE TRIGGER message_thread_scope BEFORE INSERT ON messages
WHEN NEW.thread_root_id IS NOT NULL AND NOT EXISTS(
 SELECT 1 FROM messages m JOIN channels c ON c.id=m.channel_id
 WHERE m.id=NEW.thread_root_id AND m.channel_id=NEW.channel_id AND m.thread_root_id IS NULL
 AND c.kind='text')
BEGIN SELECT RAISE(ABORT,'thread must belong to a text message'); END;
CREATE TABLE thread_subscriptions (
 message_id INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 following INTEGER NOT NULL DEFAULT 1 CHECK(following IN (0,1)),
 last_read_id INTEGER NOT NULL DEFAULT 0,
 explicit INTEGER NOT NULL DEFAULT 0 CHECK(explicit IN (0,1)),
 PRIMARY KEY(message_id,user_id)
);
CREATE INDEX thread_subscriptions_user ON thread_subscriptions(user_id,following,message_id);
ALTER TABLE channel_notifications ADD COLUMN read_at INTEGER;
CREATE INDEX inbox_user ON channel_notifications(user_id,read_at,message_id DESC);

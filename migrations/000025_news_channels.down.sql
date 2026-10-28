-- Comments are removed before dropping thread metadata. News posts remain text.
DELETE FROM messages WHERE thread_root_id IS NOT NULL;
DROP TRIGGER message_comment_delete;
DROP TRIGGER message_comment_insert;
DROP TRIGGER message_thread_scope;
DROP INDEX idx_messages_thread;
ALTER TABLE messages DROP COLUMN thread_root_id;
DROP TRIGGER channel_publisher_scope;
DROP TABLE channel_publishers;
ALTER TABLE channels DROP COLUMN comments_enabled;
ALTER TABLE channels DROP COLUMN is_news;

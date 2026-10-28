CREATE TABLE storage_budgets (
 category TEXT PRIMARY KEY CHECK(category IN ('images','files','recordings')),
 quota_bytes INTEGER NOT NULL CHECK(quota_bytes>=0)
);
CREATE TABLE attachments (
 id TEXT PRIMARY KEY,
 channel_id INTEGER REFERENCES channels(id) ON DELETE CASCADE,
 conversation_id INTEGER REFERENCES conversations(id) ON DELETE CASCADE,
 owner_user_id INTEGER NOT NULL REFERENCES users(id),
 message_id INTEGER REFERENCES messages(id) ON DELETE SET NULL,
 direct_message_id INTEGER REFERENCES conversation_messages(id) ON DELETE SET NULL,
 filename TEXT NOT NULL, mime_type TEXT NOT NULL,
 category TEXT NOT NULL CHECK(category IN ('images','files')),
 size_bytes INTEGER NOT NULL CHECK(size_bytes>=0),
 created_at INTEGER NOT NULL DEFAULT(unixepoch()),
 deleted_at INTEGER,
 CHECK((channel_id IS NOT NULL AND conversation_id IS NULL AND direct_message_id IS NULL) OR (conversation_id IS NOT NULL AND channel_id IS NULL AND message_id IS NULL))
);
CREATE INDEX attachments_storage ON attachments(category,deleted_at,created_at);
CREATE INDEX attachments_message ON attachments(message_id);
CREATE INDEX attachments_direct_message ON attachments(direct_message_id);
CREATE TRIGGER attachment_message_revision AFTER UPDATE OF deleted_at ON attachments
WHEN NEW.message_id IS NOT NULL AND NEW.deleted_at IS NOT OLD.deleted_at
BEGIN UPDATE messages SET revision=revision+1 WHERE id=NEW.message_id; END;

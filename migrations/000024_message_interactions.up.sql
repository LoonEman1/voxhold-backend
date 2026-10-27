ALTER TABLE message_send_keys ADD COLUMN request_kind TEXT NOT NULL DEFAULT 'message' CHECK(request_kind IN ('message','reply','forward'));
ALTER TABLE messages ADD COLUMN reply_to_message_id INTEGER REFERENCES messages(id) ON DELETE SET NULL;
ALTER TABLE messages ADD COLUMN reply_to_original_id INTEGER;
ALTER TABLE messages ADD COLUMN forward_data TEXT CHECK (forward_data IS NULL OR json_valid(forward_data));
CREATE INDEX idx_messages_reply_target ON messages(reply_to_message_id) WHERE reply_to_message_id IS NOT NULL;

CREATE TRIGGER messages_reply_scope BEFORE INSERT ON messages
WHEN NEW.reply_to_message_id IS NOT NULL AND NOT EXISTS (
    SELECT 1 FROM messages WHERE id=NEW.reply_to_message_id AND channel_id=NEW.channel_id
)
BEGIN SELECT RAISE(ABORT, 'reply must refer to the same channel'); END;

CREATE TRIGGER messages_reply_deleted AFTER UPDATE OF reply_to_message_id ON messages
WHEN OLD.reply_to_message_id IS NOT NULL AND NEW.reply_to_message_id IS NULL
BEGIN UPDATE messages SET revision=revision+1 WHERE id=NEW.id; END;

CREATE TRIGGER messages_reply_edited AFTER UPDATE OF content ON messages
WHEN OLD.content<>NEW.content
BEGIN UPDATE messages SET revision=revision+1 WHERE reply_to_message_id=NEW.id; END;

CREATE TABLE message_reactions (
    message_id INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    channel_id INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    server_id INTEGER NOT NULL,
    user_id INTEGER NOT NULL,
    emoji TEXT NOT NULL CHECK(length(emoji) BETWEEN 1 AND 16),
    created_at INTEGER NOT NULL DEFAULT (unixepoch()),
    PRIMARY KEY(message_id, emoji, user_id),
    FOREIGN KEY(server_id,user_id) REFERENCES server_members(server_id,user_id) ON DELETE CASCADE
);
CREATE INDEX idx_message_reactions_member ON message_reactions(server_id,user_id);
CREATE TRIGGER message_reaction_scope BEFORE INSERT ON message_reactions
WHEN NOT EXISTS (
    SELECT 1 FROM messages m JOIN channels c ON c.id=m.channel_id
    WHERE m.id=NEW.message_id AND m.channel_id=NEW.channel_id AND c.server_id=NEW.server_id
)
BEGIN SELECT RAISE(ABORT, 'reaction channel scope mismatch'); END;
CREATE TRIGGER message_reaction_insert AFTER INSERT ON message_reactions
BEGIN UPDATE messages SET revision=revision+1 WHERE id=NEW.message_id; END;
CREATE TRIGGER message_reaction_delete AFTER DELETE ON message_reactions
BEGIN UPDATE messages SET revision=revision+1 WHERE id=OLD.message_id; END;

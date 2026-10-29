ALTER TABLE conversations ADD COLUMN saved_owner_id INTEGER REFERENCES users(id) ON DELETE CASCADE;
CREATE UNIQUE INDEX conversations_saved_owner ON conversations(saved_owner_id) WHERE saved_owner_id IS NOT NULL;
ALTER TABLE conversation_members ADD COLUMN muted INTEGER NOT NULL DEFAULT 0 CHECK(muted IN (0,1));
ALTER TABLE conversation_messages ADD COLUMN forward_data TEXT CHECK(forward_data IS NULL OR json_valid(forward_data));
ALTER TABLE conversation_messages ADD COLUMN note TEXT NOT NULL DEFAULT '';
ALTER TABLE conversation_messages ADD COLUMN revision INTEGER NOT NULL DEFAULT 1;
CREATE TABLE conversation_reactions (
 message_id INTEGER NOT NULL REFERENCES conversation_messages(id) ON DELETE CASCADE,
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 emoji TEXT NOT NULL, PRIMARY KEY(message_id,user_id,emoji)
);
CREATE TRIGGER conversation_reaction_insert AFTER INSERT ON conversation_reactions
BEGIN UPDATE conversation_messages SET revision=revision+1 WHERE id=NEW.message_id; END;
CREATE TRIGGER conversation_reaction_delete AFTER DELETE ON conversation_reactions
BEGIN UPDATE conversation_messages SET revision=revision+1 WHERE id=OLD.message_id; END;
CREATE TABLE admin_audit (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 server_id INTEGER NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
 actor_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
 actor_name TEXT NOT NULL, action TEXT NOT NULL,
 target_type TEXT NOT NULL, target_id INTEGER NOT NULL, target_name TEXT NOT NULL DEFAULT '',
 details TEXT NOT NULL DEFAULT '{}', created_at INTEGER NOT NULL DEFAULT(unixepoch())
);
CREATE INDEX admin_audit_server ON admin_audit(server_id,id DESC);
CREATE INDEX admin_audit_actor ON admin_audit(server_id,actor_user_id,id DESC);
CREATE INDEX admin_audit_action ON admin_audit(server_id,action,id DESC);
CREATE TABLE scheduled_messages (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 destination_kind TEXT NOT NULL CHECK(destination_kind IN ('channel','conversation')),
 destination_id INTEGER NOT NULL, payload TEXT NOT NULL CHECK(json_valid(payload)),
 send_at INTEGER NOT NULL, status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','sent','failed','cancelled')),
 error TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL DEFAULT(unixepoch())
);
CREATE INDEX scheduled_messages_due ON scheduled_messages(status,send_at,id);
CREATE INDEX scheduled_messages_user ON scheduled_messages(user_id,id DESC);
CREATE TABLE polls (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 channel_message_id INTEGER UNIQUE REFERENCES messages(id) ON DELETE CASCADE,
 direct_message_id INTEGER UNIQUE REFERENCES conversation_messages(id) ON DELETE CASCADE,
 question TEXT NOT NULL, multiple INTEGER NOT NULL DEFAULT 0, anonymous INTEGER NOT NULL DEFAULT 0,
 closes_at INTEGER NOT NULL, revision INTEGER NOT NULL DEFAULT 1,
 CHECK((channel_message_id IS NOT NULL) <> (direct_message_id IS NOT NULL))
);
CREATE TABLE poll_options (
 id INTEGER PRIMARY KEY AUTOINCREMENT, poll_id INTEGER NOT NULL REFERENCES polls(id) ON DELETE CASCADE,
 label TEXT NOT NULL, UNIQUE(id,poll_id)
);
CREATE TABLE poll_votes (
 poll_id INTEGER NOT NULL REFERENCES polls(id) ON DELETE CASCADE,
 option_id INTEGER NOT NULL, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 PRIMARY KEY(poll_id,option_id,user_id),
 FOREIGN KEY(option_id,poll_id) REFERENCES poll_options(id,poll_id) ON DELETE CASCADE
);
CREATE TABLE server_events (
 id INTEGER PRIMARY KEY AUTOINCREMENT, server_id INTEGER NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
 creator_user_id INTEGER NOT NULL REFERENCES users(id), title TEXT NOT NULL, description TEXT NOT NULL DEFAULT '',
 starts_at INTEGER NOT NULL, timezone TEXT NOT NULL DEFAULT 'UTC', cancelled INTEGER NOT NULL DEFAULT 0,
 revision INTEGER NOT NULL DEFAULT 1, created_at INTEGER NOT NULL DEFAULT(unixepoch())
);
CREATE INDEX server_events_upcoming ON server_events(server_id,starts_at,id);
CREATE TABLE server_event_responses (
 event_id INTEGER NOT NULL REFERENCES server_events(id) ON DELETE CASCADE,
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 response TEXT NOT NULL CHECK(response IN ('going','maybe')),
 reminded INTEGER NOT NULL DEFAULT 0, PRIMARY KEY(event_id,user_id)
);
ALTER TABLE conversations ADD COLUMN call_revision INTEGER NOT NULL DEFAULT 0;
CREATE TABLE conversation_call_members (
 conversation_id INTEGER NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
 user_id INTEGER NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
 session_id TEXT NOT NULL, joined_at INTEGER NOT NULL DEFAULT(unixepoch()),
 heartbeat_at INTEGER NOT NULL DEFAULT(unixepoch()),
 PRIMARY KEY(conversation_id,user_id),
 FOREIGN KEY(conversation_id,user_id) REFERENCES conversation_members(conversation_id,user_id) ON DELETE CASCADE
);

CREATE INDEX poll_votes_option ON poll_votes(option_id,user_id);
CREATE UNIQUE INDEX scheduled_message_keys ON scheduled_messages(user_id,json_extract(payload,'$.client_message_id')) WHERE COALESCE(json_extract(payload,'$.client_message_id'),'')<>'';

CREATE INDEX server_events_due ON server_events(cancelled,starts_at,id);

CREATE TRIGGER conversation_call_join AFTER INSERT ON conversation_call_members
BEGIN UPDATE conversations SET call_revision=call_revision+1 WHERE id=NEW.conversation_id; END;
CREATE TRIGGER conversation_call_leave AFTER DELETE ON conversation_call_members
BEGIN UPDATE conversations SET call_revision=call_revision+1 WHERE id=OLD.conversation_id; END;
CREATE TRIGGER conversation_call_replace AFTER UPDATE OF session_id ON conversation_call_members
WHEN OLD.session_id <> NEW.session_id
BEGIN UPDATE conversations SET call_revision=call_revision+1 WHERE id=NEW.conversation_id; END;

-- Account deletion anonymizes the user row; remove private feature data at the same time.
CREATE TRIGGER community_account_cleanup AFTER UPDATE OF deleted_at ON users
WHEN OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL
BEGIN
 DELETE FROM conversations WHERE saved_owner_id=NEW.id;
 DELETE FROM scheduled_messages WHERE user_id=NEW.id;
 DELETE FROM conversation_call_members WHERE user_id=NEW.id;
 DELETE FROM server_event_responses WHERE user_id=NEW.id;
 DELETE FROM conversation_reactions WHERE user_id=NEW.id;
 DELETE FROM poll_votes WHERE user_id=NEW.id;
 UPDATE server_events SET cancelled=1,revision=revision+1 WHERE creator_user_id=NEW.id AND cancelled=0;
END;

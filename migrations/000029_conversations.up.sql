CREATE TABLE message_privacy (
 user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 mode TEXT NOT NULL CHECK(mode IN ('shared','everyone','none'))
);
CREATE TABLE user_blocks (
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 blocked_user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 CHECK(user_id<>blocked_user_id), PRIMARY KEY(user_id,blocked_user_id)
);
CREATE TABLE conversations (
 id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL DEFAULT '',
 created_by INTEGER NOT NULL REFERENCES users(id), direct_key TEXT UNIQUE,
 created_at INTEGER NOT NULL DEFAULT(unixepoch())
);
CREATE TABLE conversation_members (
 conversation_id INTEGER NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 last_read_id INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(conversation_id,user_id)
);
CREATE INDEX conversation_members_user ON conversation_members(user_id,conversation_id);
CREATE TABLE conversation_messages (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 conversation_id INTEGER NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
 author_user_id INTEGER NOT NULL REFERENCES users(id),
 content TEXT NOT NULL CHECK(length(content) BETWEEN 1 AND 5120),
 client_message_id TEXT NOT NULL,
 request_hash TEXT NOT NULL,
 created_at INTEGER NOT NULL DEFAULT(unixepoch()),
 UNIQUE(conversation_id,author_user_id,client_message_id)
);
CREATE INDEX conversation_history ON conversation_messages(conversation_id,id DESC);

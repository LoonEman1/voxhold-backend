-- FEAT-13: per-device ECDH P-256 public keys. Private keys never leave the
-- client; the server only stores and serves public keys.
CREATE TABLE e2ee_device_keys (
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 device_id TEXT NOT NULL CHECK(length(device_id) = 32),
 public_key BLOB NOT NULL CHECK(length(public_key) = 65),
 created_at INTEGER NOT NULL DEFAULT(unixepoch()),
 revoked_at INTEGER,
 PRIMARY KEY(user_id, device_id)
);
CREATE INDEX e2ee_device_keys_active ON e2ee_device_keys(user_id) WHERE revoked_at IS NULL;
-- Voice channels that only accept end-to-end encrypting clients.
ALTER TABLE channels ADD COLUMN e2ee_required INTEGER NOT NULL DEFAULT 0 CHECK(e2ee_required IN (0, 1));
-- DM calls encrypt voice when every member advertises a device.
ALTER TABLE conversation_call_members ADD COLUMN e2ee_device_id TEXT;

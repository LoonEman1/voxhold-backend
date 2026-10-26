CREATE TABLE channel_recordings (
    id TEXT PRIMARY KEY,
    server_id INTEGER NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    channel_id INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    owner_user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status TEXT NOT NULL CHECK(status IN ('recording','uploading','ready','local_only','failed')),
    mime_type TEXT NOT NULL CHECK(mime_type IN ('video/webm','video/mp4')),
    size_bytes INTEGER NOT NULL DEFAULT 0 CHECK(size_bytes >= 0 AND size_bytes <= 2147483648),
    duration_ms INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL DEFAULT(unixepoch()),
    updated_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    heartbeat_at INTEGER NOT NULL DEFAULT(unixepoch())
);
CREATE UNIQUE INDEX idx_recording_active_channel ON channel_recordings(channel_id) WHERE status='recording';
CREATE UNIQUE INDEX idx_recording_active_owner ON channel_recordings(owner_user_id) WHERE status='recording';
CREATE INDEX idx_recording_server ON channel_recordings(server_id,created_at DESC);
CREATE INDEX idx_recording_expiration ON channel_recordings(expires_at);

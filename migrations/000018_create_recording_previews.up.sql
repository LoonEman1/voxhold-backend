CREATE TABLE recording_previews (
    recording_id TEXT PRIMARY KEY REFERENCES channel_recordings(id) ON DELETE CASCADE,
    content BLOB NOT NULL CHECK(length(content) <= 65536),
    etag TEXT NOT NULL
);

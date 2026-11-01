ALTER TABLE channels ADD COLUMN slowmode_seconds INTEGER NOT NULL DEFAULT 0 CHECK (slowmode_seconds BETWEEN 0 AND 21600);
-- The slowmode check reads the author's latest message in the channel; without
-- this index it scans the whole channel history for authors who rarely post.
CREATE INDEX idx_messages_channel_author ON messages(channel_id, author_user_id, id);

ALTER TABLE channels ADD COLUMN access_restricted INTEGER NOT NULL DEFAULT 0 CHECK (access_restricted IN (0, 1));

CREATE TABLE channel_user_grants (
    channel_id INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    server_id INTEGER NOT NULL,
    user_id INTEGER NOT NULL,
    PRIMARY KEY (channel_id, user_id),
    FOREIGN KEY (server_id, user_id) REFERENCES server_members(server_id, user_id) ON DELETE CASCADE
);
CREATE TRIGGER channel_user_grant_scope BEFORE INSERT ON channel_user_grants
WHEN NOT EXISTS (SELECT 1 FROM channels WHERE id = NEW.channel_id AND server_id = NEW.server_id)
BEGIN SELECT RAISE(ABORT, 'channel grant server mismatch'); END;

CREATE TABLE channel_role_grants (
    channel_id INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('owner', 'admin', 'member')),
    PRIMARY KEY (channel_id, role)
);

-- One policy for HTTP, search, read receipts, recordings and live sessions.
-- Membership is always required; administrators cannot lock themselves out.
CREATE VIEW channel_accessible_members AS
SELECT c.id AS channel_id, c.server_id, m.user_id
FROM channels c JOIN server_members m ON m.server_id = c.server_id
WHERE c.access_restricted = 0 OR m.role IN ('owner', 'admin')
   OR EXISTS (SELECT 1 FROM channel_user_grants g WHERE g.channel_id = c.id AND g.user_id = m.user_id)
   OR EXISTS (SELECT 1 FROM channel_role_grants g WHERE g.channel_id = c.id AND g.role = m.role);

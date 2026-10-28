CREATE TABLE server_roles (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 server_id INTEGER NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
 name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 64),
 color TEXT NOT NULL DEFAULT '#8b93a7',
 permissions INTEGER NOT NULL CHECK(permissions BETWEEN 0 AND 31),
 UNIQUE(server_id,name), UNIQUE(server_id,id)
);
-- Bits: read=1, publish=2, voice=4, stream=8, manage_messages=16.
-- With no custom assignments the legacy member permissions remain 15.
CREATE TABLE member_roles (
 server_id INTEGER NOT NULL, user_id INTEGER NOT NULL, role_id INTEGER NOT NULL,
 PRIMARY KEY(server_id,user_id,role_id),
 FOREIGN KEY(server_id,user_id) REFERENCES server_members(server_id,user_id) ON DELETE CASCADE,
 FOREIGN KEY(server_id,role_id) REFERENCES server_roles(server_id,id) ON DELETE CASCADE
);
CREATE INDEX member_roles_role ON member_roles(role_id);
CREATE VIEW member_permissions AS
SELECT m.server_id,m.user_id,
 CASE WHEN m.role IN ('owner','admin') THEN 31
 WHEN NOT EXISTS(SELECT 1 FROM member_roles a WHERE a.server_id=m.server_id AND a.user_id=m.user_id) THEN 15
 ELSE COALESCE((SELECT max(r.permissions & 1)+max(r.permissions & 2)+max(r.permissions & 4)+max(r.permissions & 8)+max(r.permissions & 16)
 FROM member_roles a JOIN server_roles r ON r.id=a.role_id WHERE a.server_id=m.server_id AND a.user_id=m.user_id),0) END AS permissions
FROM server_members m;
CREATE TABLE channel_custom_role_grants (
 channel_id INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
 role_id INTEGER NOT NULL REFERENCES server_roles(id) ON DELETE CASCADE,
 PRIMARY KEY(channel_id,role_id)
);
CREATE TRIGGER custom_role_grant_scope BEFORE INSERT ON channel_custom_role_grants
WHEN NOT EXISTS(SELECT 1 FROM channels c JOIN server_roles r ON r.server_id=c.server_id WHERE c.id=NEW.channel_id AND r.id=NEW.role_id)
BEGIN SELECT RAISE(ABORT,'role grant scope mismatch'); END;
DROP VIEW channel_accessible_members;
CREATE VIEW channel_accessible_members AS
SELECT c.id AS channel_id,c.server_id,m.user_id FROM channels c
JOIN server_members m ON m.server_id=c.server_id
JOIN member_permissions p ON p.server_id=m.server_id AND p.user_id=m.user_id
WHERE (p.permissions & 1)<>0 AND (c.access_restricted=0 OR m.role IN ('owner','admin')
 OR EXISTS(SELECT 1 FROM channel_user_grants g WHERE g.channel_id=c.id AND g.user_id=m.user_id)
 OR EXISTS(SELECT 1 FROM channel_role_grants g WHERE g.channel_id=c.id AND g.role=m.role)
 OR EXISTS(SELECT 1 FROM channel_custom_role_grants g JOIN member_roles a ON a.role_id=g.role_id WHERE g.channel_id=c.id AND a.server_id=c.server_id AND a.user_id=m.user_id));

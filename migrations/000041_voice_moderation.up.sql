ALTER TABLE server_roles ADD COLUMN voice_permissions INTEGER NOT NULL DEFAULT 0 CHECK((voice_permissions & ~224)=0);

CREATE TABLE server_voice_mutes (
 server_id INTEGER NOT NULL,
 user_id INTEGER NOT NULL,
 PRIMARY KEY(server_id,user_id),
 FOREIGN KEY(server_id,user_id) REFERENCES server_members(server_id,user_id) ON DELETE CASCADE
);

DROP VIEW member_permissions;
CREATE VIEW member_permissions AS
SELECT server_id,user_id,
 CASE WHEN timed_out THEN base & 17 ELSE base END AS permissions
FROM (
 SELECT m.server_id,m.user_id,
  (m.timeout_until IS NOT NULL AND m.timeout_until > unixepoch()) AS timed_out,
  CASE WHEN m.role IN ('owner','admin') THEN 255
  WHEN NOT EXISTS(SELECT 1 FROM member_roles a WHERE a.server_id=m.server_id AND a.user_id=m.user_id) THEN 15
  ELSE COALESCE((SELECT max(r.permissions & 1)+max(r.permissions & 2)+max(r.permissions & 4)+max(r.permissions & 8)+max(r.permissions & 16)
    +max(r.voice_permissions & 32)+max(r.voice_permissions & 64)+max(r.voice_permissions & 128)
  FROM member_roles a JOIN server_roles r ON r.id=a.role_id WHERE a.server_id=m.server_id AND a.user_id=m.user_id),0) END AS base
 FROM server_members m
);

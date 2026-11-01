ALTER TABLE server_members ADD COLUMN timeout_until INTEGER;
-- A timed-out member keeps Read and ManageMessages (17) but loses Publish, Voice and Stream.
DROP VIEW member_permissions;
CREATE VIEW member_permissions AS
SELECT server_id,user_id,
 CASE WHEN timed_out THEN base & 17 ELSE base END AS permissions
FROM (
 SELECT m.server_id,m.user_id,
  (m.timeout_until IS NOT NULL AND m.timeout_until > unixepoch()) AS timed_out,
  CASE WHEN m.role IN ('owner','admin') THEN 31
  WHEN NOT EXISTS(SELECT 1 FROM member_roles a WHERE a.server_id=m.server_id AND a.user_id=m.user_id) THEN 15
  ELSE COALESCE((SELECT max(r.permissions & 1)+max(r.permissions & 2)+max(r.permissions & 4)+max(r.permissions & 8)+max(r.permissions & 16)
  FROM member_roles a JOIN server_roles r ON r.id=a.role_id WHERE a.server_id=m.server_id AND a.user_id=m.user_id),0) END AS base
 FROM server_members m
);

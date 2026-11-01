DROP VIEW member_permissions;
CREATE VIEW member_permissions AS
SELECT m.server_id,m.user_id,
 CASE WHEN m.role IN ('owner','admin') THEN 31
 WHEN NOT EXISTS(SELECT 1 FROM member_roles a WHERE a.server_id=m.server_id AND a.user_id=m.user_id) THEN 15
 ELSE COALESCE((SELECT max(r.permissions & 1)+max(r.permissions & 2)+max(r.permissions & 4)+max(r.permissions & 8)+max(r.permissions & 16)
 FROM member_roles a JOIN server_roles r ON r.id=a.role_id WHERE a.server_id=m.server_id AND a.user_id=m.user_id),0) END AS permissions
FROM server_members m;
ALTER TABLE server_members DROP COLUMN timeout_until;

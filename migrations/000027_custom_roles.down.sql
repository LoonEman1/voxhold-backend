DROP VIEW channel_accessible_members;
DROP TABLE channel_custom_role_grants;
DROP VIEW member_permissions;
DROP TABLE member_roles;
DROP TABLE server_roles;
CREATE VIEW channel_accessible_members AS
SELECT c.id AS channel_id,c.server_id,m.user_id FROM channels c JOIN server_members m ON m.server_id=c.server_id
WHERE c.access_restricted=0 OR m.role IN ('owner','admin')
 OR EXISTS(SELECT 1 FROM channel_user_grants g WHERE g.channel_id=c.id AND g.user_id=m.user_id)
 OR EXISTS(SELECT 1 FROM channel_role_grants g WHERE g.channel_id=c.id AND g.role=m.role);

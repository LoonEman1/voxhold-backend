DROP INDEX IF EXISTS idx_server_invites_server;

DROP INDEX IF EXISTS idx_server_invites_inviter;

DROP INDEX IF EXISTS idx_server_invites_invitee_status;

DROP INDEX IF EXISTS idx_server_invites_pending;

DROP TABLE IF EXISTS server_invites;

DROP INDEX IF EXISTS idx_server_invite_links_expires;

DROP INDEX IF EXISTS idx_server_invite_links_server;

DROP TABLE IF EXISTS server_invite_links;
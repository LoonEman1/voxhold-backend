DROP INDEX IF EXISTS idx_user_bans_banned_by;
DROP TABLE IF EXISTS user_bans;

ALTER TABLE users
DROP COLUMN deleted_at;

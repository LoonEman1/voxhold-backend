ALTER TABLE users ADD COLUMN username_key TEXT;
CREATE UNIQUE INDEX idx_users_username_key ON users(username_key) WHERE username_key IS NOT NULL;

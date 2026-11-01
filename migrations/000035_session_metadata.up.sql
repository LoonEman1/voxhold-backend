ALTER TABLE sessions ADD COLUMN last_used_at INTEGER;
ALTER TABLE sessions ADD COLUMN user_agent TEXT;
ALTER TABLE sessions ADD COLUMN client_ip TEXT;

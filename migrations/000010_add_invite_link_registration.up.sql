ALTER TABLE server_invite_links
ADD COLUMN allow_registration INTEGER NOT NULL DEFAULT 0
    CHECK (allow_registration IN (0, 1));

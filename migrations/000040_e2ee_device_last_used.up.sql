-- FEAT-13: clients re-register their device on every encrypted call, so a full
-- key list can drop the device that was used least recently instead of locking
-- the user out (private windows and cleared browsers never revoke theirs).
ALTER TABLE e2ee_device_keys ADD COLUMN last_used_at INTEGER NOT NULL DEFAULT 0;

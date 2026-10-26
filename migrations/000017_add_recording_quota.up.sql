-- NULL keeps RECORDING_STORAGE_QUOTA_BYTES (6 GiB by default); the instance owner can override it.
ALTER TABLE instance_settings
    ADD COLUMN recording_quota_bytes INTEGER
        CHECK (recording_quota_bytes IS NULL OR recording_quota_bytes > 0);

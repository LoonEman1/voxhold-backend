ALTER TABLE attachments ADD COLUMN width INTEGER CHECK(width IS NULL OR width > 0);
ALTER TABLE attachments ADD COLUMN height INTEGER CHECK(height IS NULL OR height > 0);
CREATE INDEX attachments_usage ON attachments(deleted_at, category, size_bytes);

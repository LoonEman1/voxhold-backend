ALTER TABLE conversation_call_members DROP COLUMN e2ee_device_id;
ALTER TABLE channels DROP COLUMN e2ee_required;
DROP INDEX IF EXISTS e2ee_device_keys_active;
DROP TABLE IF EXISTS e2ee_device_keys;

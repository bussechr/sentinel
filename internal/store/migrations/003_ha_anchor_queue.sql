-- sentinel v1 schema migration - HA anchor queue metadata
-- Run with: psql $SENTINEL_POSTGRES_DSN -f 003_ha_anchor_queue.sql

BEGIN;

ALTER TABLE anchor_queue ADD COLUMN IF NOT EXISTS correlation_id TEXT;
ALTER TABLE anchor_queue ADD COLUMN IF NOT EXISTS app_id TEXT;
ALTER TABLE anchor_queue ADD COLUMN IF NOT EXISTS locked_at TIMESTAMPTZ;
ALTER TABLE anchor_queue ADD COLUMN IF NOT EXISTS anchored_at TIMESTAMPTZ;
ALTER TABLE anchor_queue ADD COLUMN IF NOT EXISTS last_error TEXT;

CREATE INDEX IF NOT EXISTS idx_anchor_queue_claim
    ON anchor_queue (status, queued_at, id);

COMMIT;

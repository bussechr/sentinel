-- sentinel v1 schema migration — replay-friendly receipts + shadow policy log
-- Run with: psql $SENTINEL_POSTGRES_DSN -f 002_replay_and_shadow.sql

BEGIN;

-- ───────────────────────────────────────────────
-- Receipt enrichments (KYB-chain replay pattern)
-- ───────────────────────────────────────────────
ALTER TABLE receipts ADD COLUMN IF NOT EXISTS correlation_id     TEXT;
ALTER TABLE receipts ADD COLUMN IF NOT EXISTS evidence_root_hash TEXT;
ALTER TABLE receipts ADD COLUMN IF NOT EXISTS writer_kind        TEXT;
ALTER TABLE receipts ADD COLUMN IF NOT EXISTS writer_name        TEXT;

CREATE INDEX IF NOT EXISTS idx_receipts_correlation_id ON receipts (correlation_id);
CREATE INDEX IF NOT EXISTS idx_receipts_writer_kind    ON receipts (writer_kind);

-- ───────────────────────────────────────────────
-- Shadow decisions — diff between active and candidate bundles
-- ───────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS shadow_decisions (
    shadow_id          TEXT        PRIMARY KEY,
    packet_id          TEXT        NOT NULL REFERENCES packets(packet_id),
    correlation_id     TEXT        NOT NULL,
    active_bundle_id   TEXT        NOT NULL,
    candidate_bundle_id TEXT       NOT NULL,
    active_decision    TEXT        NOT NULL,
    candidate_decision TEXT        NOT NULL,
    diverged           BOOLEAN     NOT NULL,
    active_reason      TEXT,
    candidate_reason   TEXT,
    evaluated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_shadow_correlation ON shadow_decisions (correlation_id);
CREATE INDEX IF NOT EXISTS idx_shadow_diverged    ON shadow_decisions (diverged);
CREATE INDEX IF NOT EXISTS idx_shadow_evaluated   ON shadow_decisions (evaluated_at DESC);

-- ───────────────────────────────────────────────
-- Cold archive index — pointers to evidence retired past the 72h window
-- ───────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS cold_archive_index (
    archive_id        TEXT        PRIMARY KEY,
    correlation_id    TEXT        NOT NULL,
    app_id            TEXT        NOT NULL,
    archived_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    object_uri        TEXT        NOT NULL,
    record_count      INT         NOT NULL DEFAULT 0,
    bundle_hash       TEXT,
    proof_locator     TEXT
);

CREATE INDEX IF NOT EXISTS idx_cold_correlation ON cold_archive_index (correlation_id);
CREATE INDEX IF NOT EXISTS idx_cold_app_id      ON cold_archive_index (app_id);
CREATE INDEX IF NOT EXISTS idx_cold_archived_at ON cold_archive_index (archived_at DESC);

COMMIT;

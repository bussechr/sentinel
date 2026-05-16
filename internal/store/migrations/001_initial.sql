-- sentinel v1 schema migrations
-- Run with: psql $SENTINEL_POSTGRES_DSN -f 001_initial.sql

BEGIN;

-- ───────────────────────────────────────────────
-- Application registry
-- ───────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS apps (
    app_id              TEXT        PRIMARY KEY,
    service             TEXT        NOT NULL,
    environment         TEXT        NOT NULL,
    owner               TEXT        NOT NULL,
    mode                TEXT        NOT NULL DEFAULT 'observe',
    risk_tier           TEXT        NOT NULL DEFAULT 'low',
    allowed_categories  TEXT[]      NOT NULL DEFAULT '{}',
    policy_scope        TEXT        NOT NULL DEFAULT 'default',
    signing_key_ref     TEXT        NOT NULL,
    registration_token  TEXT        NOT NULL,
    registered_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at        TIMESTAMPTZ
);

-- ───────────────────────────────────────────────
-- Governance packets (hot index — 72-hour window)
-- ───────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS packets (
    packet_id          TEXT        PRIMARY KEY,
    correlation_id     TEXT        NOT NULL,
    trace_id           TEXT,
    captured_at        TIMESTAMPTZ NOT NULL,
    app_id             TEXT        NOT NULL REFERENCES apps(app_id),
    actor_type         TEXT        NOT NULL,
    actor_id_hash      TEXT        NOT NULL,
    identity_provider  TEXT        NOT NULL,
    action_category    TEXT        NOT NULL,
    action_name        TEXT        NOT NULL,
    risk               TEXT        NOT NULL,
    mutating           BOOLEAN     NOT NULL DEFAULT FALSE,
    resource_type      TEXT,
    resource_id_hash   TEXT,
    tenant_hash        TEXT,
    body_hash          TEXT        NOT NULL,
    redaction_profile  TEXT        NOT NULL,
    object_uri         TEXT,
    is_ai_related      BOOLEAN     NOT NULL DEFAULT FALSE,
    tool_call_count    INT         NOT NULL DEFAULT 0,
    schema_version     TEXT        NOT NULL DEFAULT 'sentinel.packet.v1',
    raw                JSONB       NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_packets_correlation ON packets (correlation_id);
CREATE INDEX IF NOT EXISTS idx_packets_app_id      ON packets (app_id);
CREATE INDEX IF NOT EXISTS idx_packets_captured_at ON packets (captured_at DESC);
CREATE INDEX IF NOT EXISTS idx_packets_risk        ON packets (risk);

-- ───────────────────────────────────────────────
-- Policy decisions
-- ───────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS decisions (
    decision_id              TEXT        PRIMARY KEY,
    packet_id                TEXT        NOT NULL REFERENCES packets(packet_id),
    app_id                   TEXT        NOT NULL,
    decision                 TEXT        NOT NULL,
    reason                   TEXT,
    bundle_id                TEXT        NOT NULL,
    bundle_hash              TEXT        NOT NULL,
    decision_hash            TEXT        NOT NULL,
    masking_profile          TEXT,
    evaluated_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    opa_decision_log_path    TEXT
);

CREATE INDEX IF NOT EXISTS idx_decisions_packet_id ON decisions (packet_id);
CREATE INDEX IF NOT EXISTS idx_decisions_app_id    ON decisions (app_id);

-- ───────────────────────────────────────────────
-- Ledger receipts
-- ───────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS receipts (
    receipt_id            TEXT        PRIMARY KEY,
    packet_id             TEXT        NOT NULL REFERENCES packets(packet_id),
    packet_hash           TEXT        NOT NULL,
    decision_hash         TEXT        NOT NULL,
    policy_bundle_hash    TEXT        NOT NULL,
    app_id                TEXT        NOT NULL,
    risk                  TEXT        NOT NULL,
    anchor_mode           TEXT        NOT NULL,
    status                TEXT        NOT NULL DEFAULT 'queued',
    chain_tx_id           TEXT,
    chain_height          BIGINT,
    issued_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    verified_at           TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_receipts_packet_id ON receipts (packet_id);
CREATE INDEX IF NOT EXISTS idx_receipts_status    ON receipts (status);

-- ───────────────────────────────────────────────
-- Evidence segments
-- ───────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS evidence_segments (
    segment_id          TEXT        PRIMARY KEY,
    app_id              TEXT        NOT NULL,
    node_id             TEXT        NOT NULL,
    from_ts             TIMESTAMPTZ NOT NULL,
    to_ts               TIMESTAMPTZ NOT NULL,
    record_count        INT         NOT NULL DEFAULT 0,
    segment_hash        TEXT        NOT NULL,
    object_uri          TEXT        NOT NULL,
    redaction_profile   TEXT        NOT NULL,
    chain_anchor_id     TEXT,
    collector_status    TEXT        NOT NULL DEFAULT 'ok'
);

CREATE INDEX IF NOT EXISTS idx_segments_app_id  ON evidence_segments (app_id);
CREATE INDEX IF NOT EXISTS idx_segments_from_ts ON evidence_segments (from_ts DESC);

-- ───────────────────────────────────────────────
-- AI traces
-- ───────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS ai_traces (
    trace_id         TEXT        PRIMARY KEY,
    packet_id        TEXT        NOT NULL REFERENCES packets(packet_id),
    correlation_id   TEXT        NOT NULL,
    app_id           TEXT        NOT NULL,
    model_id_hash    TEXT,
    prompt_hash      TEXT,
    response_hash    TEXT,
    tool_call_count  INT         NOT NULL DEFAULT 0,
    decision         TEXT        NOT NULL,
    traced_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_ai_traces_correlation ON ai_traces (correlation_id);
CREATE INDEX IF NOT EXISTS idx_ai_traces_app_id      ON ai_traces (app_id);

-- ───────────────────────────────────────────────
-- Anchor queue (durable WAL substitute in Postgres)
-- ───────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS anchor_queue (
    id              BIGSERIAL   PRIMARY KEY,
    packet_id       TEXT        NOT NULL,
    packet_hash     TEXT        NOT NULL,
    decision_hash   TEXT        NOT NULL,
    bundle_hash     TEXT        NOT NULL,
    risk            TEXT        NOT NULL,
    anchor_mode     TEXT        NOT NULL,
    status          TEXT        NOT NULL DEFAULT 'pending',
    attempts        INT         NOT NULL DEFAULT 0,
    queued_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_attempt_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_anchor_queue_status ON anchor_queue (status);

-- ───────────────────────────────────────────────
-- Config / policy bundle revisions
-- ───────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS config_revisions (
    revision_id   TEXT        PRIMARY KEY,
    bundle_id     TEXT        NOT NULL,
    bundle_hash   TEXT        NOT NULL,
    bundle_url    TEXT        NOT NULL,
    promoted_by   TEXT,
    promoted_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    active        BOOLEAN     NOT NULL DEFAULT TRUE,
    promotion_forced BOOLEAN  NOT NULL DEFAULT FALSE,
    promotion_justification TEXT
);

COMMIT;

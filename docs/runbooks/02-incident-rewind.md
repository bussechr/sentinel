# Runbook: Incident Rewind

Use this runbook when investigating an incident and you need to reconstruct the
full event path for a correlation ID: app packet → policy decision (active and
shadow) → AI trace → runtime evidence → ledger receipts.

Sentinel exposes two endpoints for rewind work:

| Path | What it returns |
|---|---|
| `GET /v1/evidence/rewind/{correlationId}` | Raw arrays — packets, decisions, receipts, evidence segments |
| `GET /v1/evidence/causal/{correlationId}` | Compiled causal DAG — typed nodes and edges, `first_deny`, `anchored` |

Use the raw form to drive your own analysis; use the causal form when you
want a single timeline answer ("what blocked this?", "did the anchor
land on every backend?").

## Step 1: Identify the correlation ID

From your application logs or OTel traces, find `correlation_id`. All
Sentinel-integrated apps propagate `X-Correlation-ID` as an HTTP header
and inject it into the packet `correlation_id` field. The Go, TypeScript,
and Python SDKs all do this for you.

## Step 2: Rewind via CLI

```bash
# Raw rewind
./bin/sentinelctl rewind --correlation-id corr_01HT... --window 72h

# Causal graph (typed DAG)
./bin/sentinelctl rewind --correlation-id corr_01HT... --graph
```

Raw output contains:

- The governance packet(s)
- The policy decision record (bundle ID, decision, reason)
- AI trace records (if applicable)
- Evidence segments (runtime evidence from `sentinel-agent`)
- Ledger receipts — one per registered writer, with `writer_kind`, `writer_name`, `chain_height`

The causal graph adds:

- A typed DAG with `packet`, `decision`, `receipt`, `segment`, `ai` nodes
- `evaluated_as`, `anchored_by`, `observed_by`, `followed_by` edges
- `first_deny` — the earliest deny decision in the chain (often the root cause)
- `anchored` — whether at least one receipt has `accepted` or `anchored` status

## Step 3: Rewind via API

Direct HTTP if you are scripting from a tool other than `sentinelctl`:

```http
GET /v1/evidence/rewind/corr_01HT...?window=72h
GET /v1/evidence/causal/corr_01HT...
```

The `window` query parameter accepts any Go duration (`72h`, `24h`,
`30m`); requests larger than 72 hours fail unless export mode is used.

## Step 4: Confirm the anchor landed on every writer

Multi-backend deploys may anchor to CometBFT, Besu, and immudb in
parallel. The receipts list inside a rewind response includes every
writer; cross-check that all expected `writer_kind`s are present:

```bash
./bin/sentinelctl rewind --correlation-id corr_01HT... --json \
  | jq '.receipts[] | {writer_kind, status, chain_height, chain_tx_id}'
```

Then sanity-check writer-level health:

```bash
./bin/sentinelctl writers
```

A writer that is registered but missing from the receipts list almost
always means a transient backend outage — check writer logs and replay.

## Step 5: Export a signed evidence bundle

For formal preservation:

```bash
./bin/sentinelctl export-evidence \
  --correlation-id corr_01HT... \
  --redaction-profile default-prod
```

This produces a signed manifest. The manifest's `manifest_id` resolves
into the `cold_archive_index` table once the cold archiver runs.

## Step 6: Verify a specific ledger receipt

```bash
./bin/sentinelctl verify-ledger --packet-id pkt_01HT...
```

The CLI fetches the most recent receipt for the packet and re-verifies it
against its writer (`tx?hash=…` for CometBFT, `eth_getTransactionReceipt`
for Besu, `VerifiedGet` for immudb).

## Constraints

- The operational evidence window is **72 hours**. Queries wider than 72h require export mode.
- Payloads are redacted according to the app's redaction profile.
- If kernel/runtime evidence is missing, the segment will show `collector_status: degraded` —
  check `sentinel-agent` heartbeat logs.
- Older correlations (> 72h) live in `cold_archive_index` and the S3 manifest;
  resolving them is automatic but requires the object store to be reachable.

# Runbook: Incident Rewind

Use this runbook when investigating an incident and you need to reconstruct the full
event path for a correlation ID: app packet → policy decision → AI trace → runtime evidence → ledger receipt.

## Step 1: Identify the correlation ID

From your application logs or OTel traces, find the `correlation_id` for the request in question.
All Sentinel-integrated apps propagate `X-Correlation-ID` as an HTTP header and inject it
into the packet `correlation_id` field.

## Step 2: Rewind via CLI

```bash
bin/sentinelctl rewind \
  --correlation-id corr_01HT... \
  --window 72h
```

Expected output: a JSON structure containing:
- The governance packet
- The policy decision record (bundle ID, decision, reason)
- AI trace records (if applicable)
- Evidence segments (runtime evidence from sentinel-agent)
- Ledger receipt (chain proof)

## Step 3: Rewind via API

```http
GET /v1/evidence/rewind/corr_01HT...?window=72h
```

## Step 4: Export a signed evidence bundle

If the incident requires formal evidence preservation:

```bash
bin/sentinelctl export-evidence \
  --correlation-id corr_01HT... \
  --redaction-profile default-prod
```

This produces a signed manifest. Verify the signature:
```bash
# The signing key public component is in sentinel-signing-key secret.
# Export verification is done via the manifest_hash and signature fields.
```

## Step 5: Verify the ledger receipt

```bash
bin/sentinelctl verify-ledger \
  --packet-id pkt_01HT...
```

## Constraints

- The operational evidence window is **72 hours**. Queries wider than 72h require export mode.
- Payloads are redacted according to the app's redaction profile.
- If the kernel/runtime evidence is missing, the segment will show `collector_status: degraded`.
  This indicates sentinel-agent was unavailable during the event window — check agent heartbeat logs.

# Runbook: Policy update

Use this runbook to safely promote a new OPA policy bundle to production.
Sentinel supports two complementary review paths:

1. **One-shot simulation** — run a single packet against a candidate bundle
   on demand (`/v1/policy/simulate`).
2. **Continuous shadow diffing** — every authorise call evaluates both the
   active bundle *and* the candidate bundle in parallel; divergences are
   persisted in `shadow_decisions` and queryable via
   `/v1/policy/shadow/divergences`.

Promote to production only when both paths agree.

## Step 1: Build and sign the new bundle

```bash
cd policy/bundles/default
opa build -b . -o ../bundle-YYYYMMDD.tar.gz
opa sign --key ./policy-signing-key.pem --bundle ../bundle-YYYYMMDD.tar.gz
```

Publish the bundle to your bundle server (Nginx, OPA bundle service, S3,
or whatever your infrastructure uses). Note the URL — Sentinel needs it.

## Step 2: One-shot simulation

`simulate-policy` runs one packet against the proposed bundle URL and
returns the active vs candidate decision plus a `safe_to_promote` flag.

```bash
./bin/sentinelctl simulate-policy \
  --proposed-bundle https://policy.internal/bundle-YYYYMMDD.tar.gz \
  --packet ./policy/examples/high-risk-refund.json
```

Reject the bundle if `safe_to_promote: false`. The check is
intentionally conservative: a candidate that flips any prior `allow` to
`deny` is not safe.

## Step 3: Wire the candidate as a shadow bundle

Once the one-shot simulation passes, run the candidate **in parallel
with production traffic** before any user-visible change.

In `sentinel.yaml` (or via your config-management overlay):

```yaml
policy:
  bundle_url: file:///policy/bundles/default
  shadow_bundle_url: https://policy.internal/bundle-YYYYMMDD.tar.gz
  shadow_bundle_id: bundle-YYYYMMDD
```

Restart `sentinel-api`. From now on every `POST /v1/packets/authorize`
evaluates both bundles concurrently. The active decision is the one
returned to the caller; the candidate decision is recorded in
`shadow_decisions` with a `diverged` flag.

## Step 4: Inspect divergences

After the candidate has run for a representative sample (an hour, a day,
a peak — match it to your traffic shape):

```bash
./bin/sentinelctl shadow-divergences --since 1h --limit 50
```

Output is a list of rows where the candidate disagreed with the active
bundle, including both `reason` strings so you can see *why*. Walk the
list and check whether each divergence is a desired tightening, a bug, or
a regression.

The same data is available over HTTP for dashboards:

```http
GET /v1/policy/shadow/divergences?since=2026-04-28T12:00:00Z&limit=200
```

## Step 5: Promote

When you are satisfied:

1. Remove the `shadow_bundle_url` line from `sentinel.yaml`.
2. Update `policy.bundle_url` to the new bundle.
3. Restart `sentinel-api` (or reload OPA — the bundle server pushes,
   so often nothing else has to restart).

Confirm the active bundle:

```bash
curl http://localhost:8181/v1/policies/sentinel | jq .
```

The `revision` field should match the new bundle revision.

After emitting a test packet:

```bash
./bin/sentinelctl emit-test-packet \
  --app-id billing-api --action invoice.refund.create --risk high --mutating
```

Check the decision record in Postgres to confirm `bundle_id` matches
the new bundle.

## Step 6: Roll back

If anything is unexpected:

1. Restore the previous `policy.bundle_url`.
2. Restart (or push the previous bundle through OPA).

OPA reloads bundles in-place. No Sentinel restart is required if the
bundle server pushes the previous revision; receipts for already-emitted
packets retain the old `policy_bundle_hash`, so historical replay is
unaffected.

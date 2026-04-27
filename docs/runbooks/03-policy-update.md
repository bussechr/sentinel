# Runbook: Policy Update

Use this runbook to safely promote a new OPA policy bundle to production.

## Step 1: Build and sign the new bundle

```bash
cd policy/bundles/default
opa build -b . -o ../bundle-YYYYMMDD.tar.gz
# Sign the bundle with the policy signing key:
opa sign --key ./policy-signing-key.pem --bundle ../bundle-YYYYMMDD.tar.gz
```

## Step 2: Simulate the new bundle before promotion

```bash
bin/sentinelctl simulate-policy \
  --bundle ./policy/bundle-YYYYMMDD.tar.gz \
  --packet ./policy/examples/high-risk-refund.json
```

Review the `changed` and `safe_to_promote` fields in the output.
**Do not promote if `safe_to_promote: false`**.

## Step 3: Promote via OPA bundle push

Push the new bundle to the OPA bundle server (the URL configured in `sentinel.yaml`).
OPA will load the bundle without restarting.

## Step 4: Verify the bundle hash appears in decisions

```bash
curl http://localhost:8181/v1/policies/sentinel | jq .
```

The `revision` field should match the new bundle revision.

After emitting a test packet:
```bash
bin/sentinelctl emit-test-packet \
  --app-id billing-api \
  --action invoice.refund.create \
  --risk high \
  --mutating true
```

Check the decision record in Postgres to confirm `bundle_id` matches the new bundle.

## Step 5: Roll back

If behaviour is unexpected, re-push the previous bundle to the bundle server.
OPA will reload automatically. No Sentinel restart required.

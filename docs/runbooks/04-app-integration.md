# Runbook: App Integration

Use this runbook to connect a new application to Sentinel in under one integration cycle.

## Step 1: Register the application

```bash
bin/sentinelctl register app \
  --app-id billing-api \
  --env prod \
  --owner platform \
  --mode observe \
  --risk-tier medium
```

Save the returned `signing_key_ref` and `token` (shown once only).

## Step 2: Choose an integration mode

| Mode | When to use |
|---|---|
| Go SDK | Go applications — richest data, lowest latency |
| REST (authorize) | All other languages — use `/v1/packets/authorize` |
| HTTP proxy | Apps you cannot modify — route traffic through Sentinel |

## Step 3A: Go SDK integration

```go
import "github.com/your-org/sentinel/sdk/go/sentinel"

client := sentinel.NewClient(sentinel.Config{
    Endpoint: "http://sentinel-api:8080",
    AppID:    "billing-api",
    Mode:     sentinel.ModeObserve, // start in observe, move to guard after validation
})

mux.Handle("/refund", client.HTTPMiddleware(
    sentinel.RoutePolicy{
        ActionName: "invoice.refund.create",
        Category:   "http",
        Risk:       "high",
        Mutating:   true,
    },
    refundHandler,
))
```

## Step 3B: REST integration

```bash
curl -X POST http://sentinel-api:8080/v1/packets/authorize \
  -H "Content-Type: application/json" \
  -d '{
    "app_id": "billing-api",
    "actor_type": "service",
    "action_name": "invoice.refund.create",
    "category": "http",
    "risk": "high",
    "mutating": true,
    "payload_hash": "sha256:..."
  }'
```

## Step 4: Verify integration

```bash
bin/sentinelctl emit-test-packet \
  --app-id billing-api \
  --action invoice.refund.create \
  --risk high \
  --mutating true
```

Check that the packet appears in the evidence window:
```bash
curl "http://sentinel-api:8080/v1/evidence/window?app_id=billing-api"
```

## Step 5: Move to guard mode

Once the observe-mode decisions look correct (no false denies), promote to guard:
```bash
bin/sentinelctl register app \
  --app-id billing-api \
  --env prod \
  --owner platform \
  --mode guard
```

## Sensitive action checklist

For each action in your app, declare:
- `action_name`: `noun.resource.verb` format (e.g. `invoice.refund.create`)
- `risk`: accurate risk tier (`low | medium | high | critical`)
- `mutating`: true for any state change
- `resource_type`: what kind of data is touched
- `actor_type`: who is performing the action

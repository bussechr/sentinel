# Runbook: App integration

Use this runbook to connect a new application to Sentinel in under one
integration cycle.

## Step 1: Register the application

```bash
./bin/sentinelctl register app \
  --app-id billing-api \
  --service billing \
  --env prod \
  --owner platform \
  --mode observe \
  --risk-tier medium
```

Save the returned `signing_key_ref` and `token` (shown once only).

## Step 2: Choose an integration mode

| Path | When to use |
|---|---|
| **Go SDK** | Go services — richest data, lowest latency, drop-in HTTP middleware |
| **TypeScript SDK** | Node 20+, Bun, Cloudflare Workers, Deno — strict typed surface, Express middleware |
| **Python SDK** | Python services and AI agents — sync or asyncio variants |
| **REST direct** | Anything else — call `/v1/packets/authorize` over HTTP |
| **HTTP proxy** | Apps you cannot modify — route ingress through `sentinel-api` |

## Step 3a: Go SDK

```go
import "github.com/your-org/sentinel/sdk/go/sentinel"

client := sentinel.NewClient(sentinel.Config{
    Endpoint: "http://sentinel-api:8080",
    AppID:    "billing-api",
    Mode:     sentinel.ModeObserve, // start in observe, move to guard once decisions look right
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

The middleware reads or generates `X-Correlation-ID`, hashes the body,
calls `/v1/packets/authorize`, and propagates `X-Sentinel-Decision` and
`X-Sentinel-Packet-ID` on the response.

## Step 3b: TypeScript SDK

```ts
import { SentinelClient, withSentinel } from "@sentinel/sdk";

const sentinel = new SentinelClient({
  endpoint: "http://sentinel-api:8080",
  appId: "billing-api",
  mode: "observe",
});

app.post(
  "/refund",
  withSentinel(sentinel, {
    actionName: "invoice.refund.create",
    category: "http",
    risk: "high",
    mutating: true,
  }),
  refundHandler,
);
```

## Step 3c: Python SDK

```python
from sentinel_sdk import SentinelClient, RoutePolicy, sha256_hash

with SentinelClient("http://sentinel-api:8080", "billing-api", mode="observe") as s:
    decision = s.authorize(
        correlation_id=s.new_correlation_id(),
        policy=RoutePolicy("invoice.refund.create", "http", "high", mutating=True),
        payload_hash=sha256_hash(request_body),
    )
    if decision.decision == "deny":
        raise PermissionError(decision.reason)
```

## Step 3d: REST direct

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
    "payload_hash": "sha256:...",
    "correlation_id": "corr_..."
  }'
```

## Step 4: AI agent integration

Machine actors (`actor_type: "agent" | "model"`) and any packet with
`category: "ai" | "tool"` **must** transit the AI lane. The control plane
rejects bypass attempts with HTTP 403.

The AI lane is enforced by two headers:

| Header | Required value |
|---|---|
| `X-Sentinel-AI-Route` | `ai-gateway` |
| `X-Sentinel-AI-Actor` | non-empty (e.g. `model:claude`, `agent:billing-bot`) |

The TypeScript and Python SDKs attach both headers automatically when you
call `aiAuthorize` / `ai_authorize` and `aiResult` / `ai_result`.

```python
authz = s.ai_authorize(
    correlation_id=corr,
    model_id_hash=sha256_hash(model_id),
    prompt_hash=sha256_hash(prompt_text),
)
if authz.decision == "deny":
    raise PermissionError(authz.reason)

# … invoke the model …

s.ai_result(
    correlation_id=corr,
    response_hash=sha256_hash(response_text),
    tool_call_count=len(tool_calls),
    packet_id=authz.packet_id,
)
```

See the working examples under [examples/anthropic-ai-lane](../../examples/anthropic-ai-lane)
and [examples/openai-compatible-ai-lane](../../examples/openai-compatible-ai-lane).

## Step 5: Verify the integration

Emit a test packet from the CLI side:

```bash
./bin/sentinelctl emit-test-packet \
  --app-id billing-api \
  --action invoice.refund.create \
  --risk high \
  --mutating
```

Confirm the packet shows up in the hot index:

```bash
curl "http://sentinel-api:8080/v1/evidence/window?app_id=billing-api"
```

Confirm receipts fanned out to every registered ledger writer:

```bash
./bin/sentinelctl writers
./bin/sentinelctl rewind --correlation-id corr_... --graph
```

## Step 6: Move to guard mode

Once the observe-mode decisions look correct (no false denies):

```bash
./bin/sentinelctl register app \
  --app-id billing-api --service billing --env prod --owner platform --mode guard
```

Re-run the test packet from step 5 to confirm `guard` denies the
right things.

## Action declaration checklist

For every action in your app, declare:

- `action_name`: `noun.resource.verb` (e.g. `invoice.refund.create`)
- `risk`: accurate tier (`low | medium | high | critical`)
- `mutating`: true for any state change
- `resource_type`: what kind of data is touched
- `actor_type`: `service` for app code, `agent`/`model` for AI agents/models
- `category`: `http | grpc | ai | tool | db | file | network | k8s | secret | config`

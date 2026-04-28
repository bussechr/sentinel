# Sentinel Python SDK

Sync and async clients for the Sentinel control plane.

## Install

This SDK lives inside the Sentinel monorepo. From a checkout:

```bash
pip install -e ./sdk/python
```

Or, after publishing to your private index:

```bash
pip install sentinel-sdk
```

## Quickstart

```python
from sentinel_sdk import SentinelClient, RoutePolicy, sha256_hash

with SentinelClient(
    endpoint="http://sentinel-api:8080",
    app_id="billing-api",
    mode="guard",
    token="…",
) as sentinel:
    decision = sentinel.authorize(
        correlation_id=sentinel.new_correlation_id(),
        policy=RoutePolicy(
            action_name="invoice.refund.create",
            category="http",
            risk="high",
            mutating=True,
        ),
        payload_hash=sha256_hash(request_body),
    )
    if decision.decision == "deny":
        raise PermissionError(decision.reason)
```

## Async

```python
from sentinel_sdk import AsyncSentinelClient, RoutePolicy, sha256_hash

async def main():
    async with AsyncSentinelClient("http://sentinel-api:8080", "billing-api", mode="guard") as s:
        result = await s.authorize(
            "corr_123",
            RoutePolicy("invoice.refund.create", "http", "high", mutating=True),
            sha256_hash("payload"),
        )
        print(result.decision, result.reason)
```

## AI lane

```python
authz = sentinel.ai_authorize(
    correlation_id=corr,
    model_id_hash=sha256_hash(model_id),
    prompt_hash=sha256_hash(prompt),
)
if authz.decision == "deny":
    raise PermissionError(authz.reason)

# … invoke the model …

sentinel.ai_result(
    correlation_id=corr,
    response_hash=sha256_hash(response_text),
    tool_call_count=len(tool_calls),
    packet_id=authz.packet_id,
)
```

The SDK automatically attaches the `X-Sentinel-AI-Route` and
`X-Sentinel-AI-Actor` headers required by the Sentinel AI gateway.

## Surface

| Method | What it calls |
|---|---|
| `authorize(correlation_id, policy, payload_hash, …)` | `POST /v1/packets/authorize` |
| `ai_authorize(correlation_id, model_id_hash, prompt_hash, …)` | `POST /v1/ai/authorize` |
| `ai_result(correlation_id, response_hash, tool_call_count, …)` | `POST /v1/ai/result` |
| `rewind(correlation_id, window=…)` | `GET /v1/evidence/rewind/{cid}` |
| `causal_graph(correlation_id)` | `GET /v1/evidence/causal/{cid}` |
| `list_writers()` | `GET /v1/ledger/writers` |
| `shadow_divergences(since=…, limit=…)` | `GET /v1/policy/shadow/divergences` |
| `sha256_hash(value)` (module-level) | `"sha256:<hex>"` |

## Mode semantics

| Mode | Allow / Warn | Deny | API unreachable |
|---|---|---|---|
| `observe` | proceed | proceed (advisory) | returns synthetic `decision_id="degraded"` |
| `guard` | proceed | raises `SentinelError` from caller logic | raises |
| `enforce` | proceed only after receipt acknowledged | raises | raises |

## Requirements

- Python ≥ 3.10
- `httpx` ≥ 0.27

## Development

```bash
cd sdk/python
pip install -e .[dev]
python -m pytest -q
```

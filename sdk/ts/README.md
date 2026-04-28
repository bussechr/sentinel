# Sentinel TypeScript SDK

Strict-mode TypeScript client for the Sentinel control plane. Runs in Node 20+,
Bun, Cloudflare Workers, Deno, and modern browsers (uses Web Crypto and the
global `fetch`).

## Install

This SDK lives inside the Sentinel monorepo. To consume it:

```bash
# from a monorepo workspace
pnpm add link:../sentinel/sdk/ts

# or, after publishing to your private registry
pnpm add @sentinel/sdk
```

Build it locally:

```bash
cd sdk/ts
npm install
npm run build
```

## Quickstart

```ts
import { SentinelClient, withSentinel } from "@sentinel/sdk";

const sentinel = new SentinelClient({
  endpoint: "http://sentinel-api:8080",
  appId: "billing-api",
  mode: "guard",
  token: process.env.SENTINEL_API_TOKEN,
});

// Express / Connect middleware
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

## Surface

| Method | What it calls |
|---|---|
| `authorize(correlationId, policy, payloadHash, extra?)` | `POST /v1/packets/authorize` |
| `aiAuthorize({ app_id, correlation_id, model_id_hash, prompt_hash, tool_call_count? })` | `POST /v1/ai/authorize` (AI gateway headers auto-attached) |
| `aiResult({ app_id, correlation_id, response_hash, tool_call_count, packet_id? })` | `POST /v1/ai/result` |
| `rewind(correlationId, window?)` | `GET /v1/evidence/rewind/{cid}` |
| `causalGraph(correlationId)` | `GET /v1/evidence/causal/{cid}` |
| `listWriters()` | `GET /v1/ledger/writers` |
| `shadowDivergences(since?, limit?)` | `GET /v1/policy/shadow/divergences` |
| `hash(value)` | sha256 (`sha256:<hex>`) over a string or `Uint8Array` |

## Mode semantics

| Mode | Allow / Warn | Deny | API unreachable |
|---|---|---|---|
| `observe` | proceed | proceed (advisory) | proceed (returns synthetic `decision_id: "degraded"`) |
| `guard` | proceed | 403 (middleware) | 503 (middleware) |
| `enforce` | proceed only after receipt acknowledged | 403 | 503 |

## AI lane

Both `aiAuthorize` and `aiResult` automatically inject the
`X-Sentinel-AI-Route: ai-gateway` and `X-Sentinel-AI-Actor: model` headers,
which the Sentinel API requires for every machine-actor call.

## Type safety

All request and response shapes are exported from `@sentinel/sdk`:

```ts
import type {
  AuthorizeRequest,
  AuthorizeResponse,
  CausalGraph,
  Decision,
  RoutePolicy,
  ShadowDivergencesResponse,
  WriterHealthList,
} from "@sentinel/sdk";
```

## Development

```bash
cd sdk/ts
npm install
npx tsc --noEmit -p tsconfig.json   # type-check
npm run build                       # emit dist/
```

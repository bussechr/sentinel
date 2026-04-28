# Sentinel Go SDK

Drop-in Go client and HTTP middleware for the Sentinel control plane.

## Install

```bash
go get github.com/your-org/sentinel/sdk/go/sentinel
```

(Replace `github.com/your-org/sentinel` with your fork's module path.)

## Quickstart

```go
import "github.com/your-org/sentinel/sdk/go/sentinel"

client := sentinel.NewClient(sentinel.Config{
    Endpoint: "http://sentinel-api:8080",
    AppID:    "billing-api",
    Mode:     sentinel.ModeGuard,
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

The middleware:

1. Reads or generates a correlation ID (`X-Correlation-ID`) and propagates it
   on the response.
2. Hashes up to 1 MB of the request body and sends it as `payload_hash`.
3. Calls `POST /v1/packets/authorize`.
4. Adds `X-Sentinel-Decision` and `X-Sentinel-Packet-ID` headers.
5. Blocks (HTTP 403) on `deny` in `guard`/`enforce` mode; passes through in
   `observe` mode (degraded calls also pass through in observe mode).

## Mode semantics

| Mode | Allow / Warn | Deny | API unreachable |
|---|---|---|---|
| `observe` | proceed | proceed (advisory) | proceed |
| `guard` | proceed | 403 | 403 |
| `enforce` | proceed only after receipt acknowledged | 403 | 403 |

## Direct calls

The middleware is optional; you can call `client.Authorize(...)` directly from
non-HTTP code paths. The returned `AuthorizeResponse` carries
`decision_id`, `packet_id`, `receipt_id`, `receipt_status`, and
`witness_id`.

## Correlation propagation

```go
ctx := r.Context()
corrID := sentinel.CorrelationIDFromContext(ctx) // populated by middleware
```

Pass `corrID` into downstream calls so Sentinel's rewind and causal graph
endpoints can stitch the chain together.

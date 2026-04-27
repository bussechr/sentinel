// Package http provides HTTP capture — it intercepts inbound and outbound HTTP
// traffic and normalises it into sentinel.packet.v1 governance packets.
//
// Two modes are supported:
//   1. Proxy mode: sentinel-api acts as a reverse proxy; all traffic passes through.
//   2. Middleware mode: the Go SDK injects capture at the handler level.
//
// M1 implementation: middleware mode only.
// M5 implementation: HTTP/gRPC transparent proxy.
package http

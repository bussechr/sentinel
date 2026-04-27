// Package observability — Prometheus-compatible metrics.
//
// Sentinel exposes the following key metrics:
//   sentinel_packets_total{app_id, risk, decision, mode}  — counter
//   sentinel_policy_eval_duration_seconds{app_id}         — histogram
//   sentinel_anchor_queue_depth                           — gauge
//   sentinel_evidence_window_hours                        — gauge
//   sentinel_chain_height                                 — gauge
//
// Metrics are emitted via OTel SDK. The OTel Collector scrapes them
// and exports to Prometheus or any other backend.
//
// This package provides the metric descriptors and a Recorder type
// that components use to record measurements without importing otel directly.
package observability

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Metrics holds all Sentinel instrumentation counters and gauges.
type Metrics struct {
	packetsTotal        metric.Int64Counter
	policyEvalHistogram metric.Float64Histogram
	anchorQueueDepth    metric.Int64ObservableGauge
}

// NewMetrics initialises all Sentinel metric instruments.
func NewMetrics() (*Metrics, error) {
	mp := otel.GetMeterProvider()
	meter := mp.Meter("sentinel")

	packetsTotal, err := meter.Int64Counter("sentinel_packets_total",
		metric.WithDescription("Total governance packets processed"),
	)
	if err != nil {
		return nil, err
	}

	policyHist, err := meter.Float64Histogram("sentinel_policy_eval_duration_seconds",
		metric.WithDescription("Policy evaluation latency"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	queueGauge, err := meter.Int64ObservableGauge("sentinel_anchor_queue_depth",
		metric.WithDescription("Number of packets waiting in the anchor queue"),
	)
	if err != nil {
		return nil, err
	}

	return &Metrics{
		packetsTotal:        packetsTotal,
		policyEvalHistogram: policyHist,
		anchorQueueDepth:    queueGauge,
	}, nil
}

// RecordPacket increments the packet counter.
func (m *Metrics) RecordPacket(ctx context.Context, appID, risk, decision, mode string) {
	if m == nil {
		return
	}
	m.packetsTotal.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("app_id", appID),
			attribute.String("risk", risk),
			attribute.String("decision", decision),
			attribute.String("mode", mode),
		),
	)
}

// RecordPolicyEval records a policy evaluation duration in seconds.
func (m *Metrics) RecordPolicyEval(ctx context.Context, appID string, durationSec float64) {
	if m == nil {
		return
	}
	m.policyEvalHistogram.Record(ctx, durationSec,
		metric.WithAttributes(attribute.String("app_id", appID)),
	)
}

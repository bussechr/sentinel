// Package observability wires OpenTelemetry tracing, metrics and health.
//
// Every Sentinel component initialises this package at startup.
// The OTLP gRPC exporter sends traces and metrics to the OTel Collector.
// The /metrics endpoint exposes Prometheus-format metrics.
// The /healthz and /readyz handlers are registered on the main HTTP mux.
package observability

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.uber.org/zap"
)

// Config holds observability configuration.
type Config struct {
	OTLPEndpoint string // e.g. "otel-collector:4317"
	ServiceName  string
	Version      string
}

// InitTracer sets up the global OTel tracer provider.
// Returns a shutdown function that must be deferred.
func InitTracer(ctx context.Context, cfg Config, log *zap.Logger) (func(context.Context) error, error) {
	exp, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
		otlptracegrpc.WithInsecure(), // use mTLS in production via WithTLSClientConfig
	)
	if err != nil {
		return nil, fmt.Errorf("otel: create OTLP exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.Version),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("otel: create resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)

	log.Info("OTel tracer initialised", zap.String("endpoint", cfg.OTLPEndpoint))

	return tp.Shutdown, nil
}

// ReadinessCheck is a function that returns nil when a dependency is healthy.
type ReadinessCheck struct {
	Name  string
	Check func(ctx context.Context) error
}

// RegisterHTTPHandlers attaches /healthz and /readyz to the mux.
func RegisterHTTPHandlers(mux *http.ServeMux, checks []ReadinessCheck) {
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		for _, c := range checks {
			if err := c.Check(ctx); err != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = fmt.Fprintf(w, `{"status":"not_ready","failed":"%s","error":"%s"}`, c.Name, err.Error())
				return
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	})
}

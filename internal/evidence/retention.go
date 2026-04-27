// Package evidence — retention enforcement.
//
// Retention.Enforce must be called on a periodic ticker (e.g. every hour).
// It deletes packet metadata from the Postgres hot index that has aged past
// the configured evidence window. Full payloads in object storage are governed
// by lifecycle policies set on the bucket, not by this component.
package evidence

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
)

// RetentionStore is the minimal interface the retention enforcer needs.
// Implemented by the Postgres store in internal/store/postgres.
type RetentionStore interface {
	// DeletePacketsOlderThan removes packet index rows older than the cutoff.
	DeletePacketsOlderThan(ctx context.Context, cutoff time.Time) (int64, error)
	// DeleteSegmentsOlderThan removes evidence segment rows older than the cutoff.
	DeleteSegmentsOlderThan(ctx context.Context, cutoff time.Time) (int64, error)
}

// Retention enforces the evidence window on the hot index.
type Retention struct {
	store    RetentionStore
	window   time.Duration
	log      *zap.Logger
}

// NewRetention creates a retention enforcer with the given window duration.
func NewRetention(store RetentionStore, window time.Duration, log *zap.Logger) *Retention {
	if window <= 0 {
		window = DefaultWindowDuration
	}
	return &Retention{store: store, window: window, log: log}
}

// Enforce deletes hot-index rows outside the window. Safe to call concurrently.
func (r *Retention) Enforce(ctx context.Context) error {
	cutoff := time.Now().UTC().Add(-r.window)

	packets, err := r.store.DeletePacketsOlderThan(ctx, cutoff)
	if err != nil {
		return fmt.Errorf("retention: delete packets: %w", err)
	}

	segments, err := r.store.DeleteSegmentsOlderThan(ctx, cutoff)
	if err != nil {
		return fmt.Errorf("retention: delete segments: %w", err)
	}

	r.log.Info("retention enforced",
		zap.Time("cutoff", cutoff),
		zap.Int64("packets_deleted", packets),
		zap.Int64("segments_deleted", segments),
	)
	return nil
}

// Package evidence — correlation ID rewind.
//
// Rewind reconstructs the full event path for a correlation ID:
//   - app packet(s)
//   - policy decision(s)
//   - AI trace (if applicable)
//   - runtime evidence segment(s)
//   - ledger receipt(s)
//
// Used by sentinelctl rewind and the incident response UI.
package evidence

import (
	"context"
	"fmt"
	"time"

	"github.com/your-org/sentinel/internal/core"
)

// RewindStore is the data interface the rewind engine needs.
type RewindStore interface {
	PacketsByCorrelationID(ctx context.Context, correlationID string, window time.Duration) ([]*core.Packet, error)
	DecisionsByCorrelationID(ctx context.Context, correlationID string, window time.Duration) ([]*core.DecisionRecord, error)
	ReceiptsByCorrelationID(ctx context.Context, correlationID string, window time.Duration) ([]*core.Receipt, error)
	SegmentsByCorrelationID(ctx context.Context, correlationID string, window time.Duration) ([]*Segment, error)
}

// RewindResult is the reconstructed event path for one correlation ID.
type RewindResult struct {
	CorrelationID string                `json:"correlation_id"`
	WindowUsed    time.Duration         `json:"window_used"`
	Packets       []*core.Packet        `json:"packets"`
	Decisions     []*core.DecisionRecord `json:"decisions"`
	Receipts      []*core.Receipt       `json:"receipts"`
	Segments      []*Segment            `json:"segments"`
}

// Rewind fetches all evidence for the correlation ID within the given window.
// window must not exceed DefaultWindowDuration unless export mode is enabled.
func Rewind(ctx context.Context, store RewindStore, correlationID string, window time.Duration, exportMode bool) (*RewindResult, error) {
	if !exportMode && window > DefaultWindowDuration {
		return nil, fmt.Errorf("rewind: window %v exceeds operational limit of %v; use export mode", window, DefaultWindowDuration)
	}
	if store == nil {
		return &RewindResult{
			CorrelationID: correlationID,
			WindowUsed:    window,
		}, nil
	}

	packets, err := store.PacketsByCorrelationID(ctx, correlationID, window)
	if err != nil {
		return nil, fmt.Errorf("rewind: packets: %w", err)
	}

	decisions, err := store.DecisionsByCorrelationID(ctx, correlationID, window)
	if err != nil {
		return nil, fmt.Errorf("rewind: decisions: %w", err)
	}

	receipts, err := store.ReceiptsByCorrelationID(ctx, correlationID, window)
	if err != nil {
		return nil, fmt.Errorf("rewind: receipts: %w", err)
	}

	segments, err := store.SegmentsByCorrelationID(ctx, correlationID, window)
	if err != nil {
		return nil, fmt.Errorf("rewind: segments: %w", err)
	}

	return &RewindResult{
		CorrelationID: correlationID,
		WindowUsed:    window,
		Packets:       packets,
		Decisions:     decisions,
		Receipts:      receipts,
		Segments:      segments,
	}, nil
}

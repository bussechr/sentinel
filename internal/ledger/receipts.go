// Package ledger — receipt persistence helpers.
//
// This file provides the Store interface that the anchor queue and API layer
// use to persist and retrieve receipts. The concrete implementation is in
// internal/store/postgres.
package ledger

import (
	"context"

	"github.com/your-org/sentinel/internal/core"
)

// ReceiptStore is the minimal interface for persisting chain receipts.
type ReceiptStore interface {
	InsertReceipt(ctx context.Context, r *core.Receipt) error
	GetReceiptByPacketID(ctx context.Context, packetID string) (*core.Receipt, error)
}

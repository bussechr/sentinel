// Package postgres — archive source implementation.
//
// Implements evidence.ArchiveSource so the cold archiver can read hot
// rows directly from Postgres before they are deleted by retention.
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/your-org/sentinel/internal/evidence"
)

// CorrelationIDsBefore returns distinct correlation IDs whose newest
// packet captured_at is older than cutoff.
func (s *Store) CorrelationIDsBefore(ctx context.Context, cutoff time.Time, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT correlation_id
		FROM packets
		GROUP BY correlation_id
		HAVING max(captured_at) < $1
		ORDER BY max(captured_at) ASC
		LIMIT $2`, cutoff, limit)
	if err != nil {
		return nil, fmt.Errorf("postgres: list correlations before cutoff: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// HotEvidence loads packets, decisions, and receipts for one correlation
// ID. Used by the archiver before retention deletes the hot rows.
func (s *Store) HotEvidence(ctx context.Context, correlationID string) (*evidence.HotEvidence, error) {
	pkts, err := s.PacketsByCorrelationID(ctx, correlationID, 365*24*time.Hour)
	if err != nil {
		return nil, err
	}
	if len(pkts) == 0 {
		return nil, nil
	}
	decs, err := s.DecisionsByCorrelationID(ctx, correlationID, 365*24*time.Hour)
	if err != nil {
		return nil, err
	}
	rcps, err := s.ReceiptsByCorrelationID(ctx, correlationID, 365*24*time.Hour)
	if err != nil {
		return nil, err
	}
	return &evidence.HotEvidence{
		Packets:   pkts,
		Decisions: decs,
		Receipts:  rcps,
	}, nil
}

// RecordArchive inserts a row into cold_archive_index. Adapter so the
// existing InsertColdArchive method satisfies evidence.ArchiveIndex.
func (s *Store) RecordArchive(ctx context.Context, rec *evidence.ArchiveRecord) error {
	return s.InsertColdArchive(ctx, &ColdArchiveRecord{
		ArchiveID:     rec.ArchiveID,
		CorrelationID: rec.CorrelationID,
		AppID:         rec.AppID,
		ArchivedAt:    rec.ArchivedAt,
		ObjectURI:     rec.ObjectURI,
		RecordCount:   rec.RecordCount,
		BundleHash:    rec.BundleHash,
		ProofLocator:  rec.ProofLocator,
	})
}

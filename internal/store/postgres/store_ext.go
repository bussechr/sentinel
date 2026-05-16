// Package postgres — extended store methods for API handlers, receipts,
// AI traces, evidence segments, and policy bundles.
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/your-org/sentinel/internal/core"
	"github.com/your-org/sentinel/internal/evidence"
)

// ─── Receipts ────────────────────────────────────────────────────────────────

// InsertReceipt stores a chain receipt. The new columns introduced in
// migration 002 (correlation_id, evidence_root_hash, writer_kind, writer_name)
// are written when present and remain NULL otherwise.
func (s *Store) InsertReceipt(ctx context.Context, r *core.Receipt) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO receipts (receipt_id, packet_id, packet_hash, decision_hash,
		    policy_bundle_hash, app_id, risk, anchor_mode, status,
		    chain_tx_id, chain_height, issued_at,
		    correlation_id, evidence_root_hash, writer_kind, writer_name)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		ON CONFLICT (receipt_id) DO NOTHING`,
		r.ReceiptID, r.PacketID, r.PacketHash, r.DecisionHash,
		r.PolicyBundleHash, r.AppID, string(r.Risk), string(r.AnchorMode),
		string(r.Status), r.ChainTransactionID, r.ChainHeight, r.IssuedAt,
		nullableString(r.CorrelationID), nullableString(r.EvidenceRootHash),
		nullableString(r.WriterKind), nullableString(r.WriterName),
	)
	return err
}

// GetReceiptByPacketID retrieves the most recent receipt for a packet.
func (s *Store) GetReceiptByPacketID(ctx context.Context, packetID string) (*core.Receipt, error) {
	var r core.Receipt
	var corrID, evRoot, wKind, wName *string
	err := s.pool.QueryRow(ctx, `
		SELECT receipt_id, packet_id, packet_hash, decision_hash,
		       policy_bundle_hash, app_id, risk, anchor_mode,
		       status, chain_tx_id, chain_height, issued_at, verified_at,
		       correlation_id, evidence_root_hash, writer_kind, writer_name
		FROM receipts WHERE packet_id = $1 ORDER BY issued_at DESC LIMIT 1`,
		packetID,
	).Scan(
		&r.ReceiptID, &r.PacketID, &r.PacketHash, &r.DecisionHash,
		&r.PolicyBundleHash, &r.AppID, &r.Risk, &r.AnchorMode,
		&r.Status, &r.ChainTransactionID, &r.ChainHeight, &r.IssuedAt, &r.VerifiedAt,
		&corrID, &evRoot, &wKind, &wName,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: get receipt for packet %q: %w", packetID, err)
	}
	r.CorrelationID = derefString(corrID)
	r.EvidenceRootHash = derefString(evRoot)
	r.WriterKind = derefString(wKind)
	r.WriterName = derefString(wName)
	return &r, nil
}

// nullableString returns nil for empty strings so Postgres stores NULL.
func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// derefString returns "" for nil pointers.
func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// ─── AI Traces ───────────────────────────────────────────────────────────────

// AITraceRecord is the minimal data needed to persist an AI trace.
type AITraceRecord struct {
	AppID         string
	CorrelationID string
	ModelIDHash   string
	PromptHash    string
	ToolCallCount int
}

// InsertAITrace persists an AI trace record.
func (s *Store) InsertAITrace(ctx context.Context, traceID string, req *AITraceRecord) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO ai_traces (trace_id, packet_id, correlation_id, app_id,
		    model_id_hash, prompt_hash, tool_call_count, decision, traced_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NOW())`,
		traceID, "", req.CorrelationID, req.AppID,
		req.ModelIDHash, req.PromptHash, req.ToolCallCount, string(core.DecisionAllow),
	)
	return err
}

// ─── Evidence Segments ───────────────────────────────────────────────────────

// InsertSegment stores an evidence segment.
func (s *Store) InsertSegment(ctx context.Context, seg *evidence.Segment) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO evidence_segments (segment_id, app_id, node_id, from_ts, to_ts,
		    record_count, segment_hash, object_uri, redaction_profile,
		    chain_anchor_id, collector_status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		seg.SegmentID, seg.AppID, seg.NodeID, seg.FromTS, seg.ToTS,
		seg.RecordCount, seg.SegmentHash, seg.ObjectURI, seg.RedactionProfile,
		seg.ChainAnchorID, seg.CollectorStatus,
	)
	return err
}

// QuerySegments returns evidence segments for an app within the time window.
func (s *Store) QuerySegments(ctx context.Context, appID string, from, to time.Time) ([]*evidence.Segment, error) {
	query := `SELECT segment_id, app_id, node_id, from_ts, to_ts,
	          record_count, segment_hash, object_uri, redaction_profile,
	          chain_anchor_id, collector_status
	          FROM evidence_segments
	          WHERE from_ts >= $1 AND to_ts <= $2`
	args := []interface{}{from, to}
	if appID != "" {
		query += " AND app_id = $3"
		args = append(args, appID)
	}
	query += " ORDER BY from_ts DESC"

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*evidence.Segment
	for rows.Next() {
		var seg evidence.Segment
		if err := rows.Scan(
			&seg.SegmentID, &seg.AppID, &seg.NodeID, &seg.FromTS, &seg.ToTS,
			&seg.RecordCount, &seg.SegmentHash, &seg.ObjectURI, &seg.RedactionProfile,
			&seg.ChainAnchorID, &seg.CollectorStatus,
		); err != nil {
			return nil, err
		}
		out = append(out, &seg)
	}
	return out, rows.Err()
}

// ─── Policy Bundles ──────────────────────────────────────────────────────────

// PolicyBundle is a row from config_revisions.
type PolicyBundle struct {
	RevisionID    string    `json:"revision_id"`
	BundleID      string    `json:"bundle_id"`
	BundleHash    string    `json:"bundle_hash"`
	BundleURL     string    `json:"bundle_url"`
	PromotedBy    string    `json:"promoted_by"`
	PromotedAt    time.Time `json:"promoted_at"`
	Active        bool      `json:"active"`
	Forced        bool      `json:"forced"`
	Justification string    `json:"justification,omitempty"`
}

// PolicyPromotion captures operator metadata for a bundle promotion.
type PolicyPromotion struct {
	PromotedBy    string
	Forced        bool
	Justification string
}

// ListPolicyBundles returns all policy bundle revisions.
func (s *Store) ListPolicyBundles(ctx context.Context) ([]*PolicyBundle, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT revision_id, bundle_id, bundle_hash, bundle_url,
		       COALESCE(promoted_by,''), promoted_at, active,
		       COALESCE(promotion_forced, FALSE), COALESCE(promotion_justification, '')
		FROM config_revisions ORDER BY promoted_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*PolicyBundle
	for rows.Next() {
		var b PolicyBundle
		if err := rows.Scan(
			&b.RevisionID, &b.BundleID, &b.BundleHash, &b.BundleURL,
			&b.PromotedBy, &b.PromotedAt, &b.Active,
			&b.Forced, &b.Justification,
		); err != nil {
			return nil, err
		}
		out = append(out, &b)
	}
	return out, rows.Err()
}

// InsertPolicyBundle records an uploaded candidate bundle revision.
func (s *Store) InsertPolicyBundle(ctx context.Context, b *PolicyBundle) error {
	if b.RevisionID == "" {
		b.RevisionID = b.BundleID
	}
	if b.PromotedAt.IsZero() {
		b.PromotedAt = time.Now().UTC()
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO config_revisions (
		    revision_id, bundle_id, bundle_hash, bundle_url,
		    promoted_by, promoted_at, active, promotion_forced,
		    promotion_justification)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (revision_id) DO UPDATE SET
		    bundle_id = EXCLUDED.bundle_id,
		    bundle_hash = EXCLUDED.bundle_hash,
		    bundle_url = EXCLUDED.bundle_url`,
		b.RevisionID, b.BundleID, b.BundleHash, b.BundleURL,
		b.PromotedBy, b.PromotedAt, b.Active, b.Forced,
		nullableString(b.Justification),
	)
	return err
}

// PromotePolicyBundle marks one bundle revision active and deactivates
// the rest. The API layer handles authorization/audit policy; the store
// keeps the state transition atomic.
func (s *Store) PromotePolicyBundle(ctx context.Context, bundleID string, promotion PolicyPromotion) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `UPDATE config_revisions SET active = FALSE WHERE active = TRUE`); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
		UPDATE config_revisions
		SET active = TRUE,
		    promoted_by = $2,
		    promoted_at = now(),
		    promotion_forced = $3,
		    promotion_justification = $4
		WHERE bundle_id = $1 OR revision_id = $1`,
		bundleID,
		promotion.PromotedBy,
		promotion.Forced,
		nullableString(promotion.Justification),
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("policy bundle %q not found", bundleID)
	}
	return tx.Commit(ctx)
}

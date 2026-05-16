package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/your-org/sentinel/internal/core"
	"github.com/your-org/sentinel/internal/ledger"
)

// EnqueueAnchor persists an anchor request before any chain writer fan-out.
func (s *Store) EnqueueAnchor(ctx context.Context, req *ledger.AnchorRequest) (int64, error) {
	if req == nil || req.Packet == nil {
		return 0, fmt.Errorf("postgres: anchor request packet required")
	}
	mode := req.Packet.Ledger.AnchorMode
	if mode == "" {
		mode = core.AnchorBatch
		if req.Packet.Action.Risk == core.RiskHigh || req.Packet.Action.Risk == core.RiskCritical {
			mode = core.AnchorImmediate
		}
	}
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO anchor_queue (
			packet_id, correlation_id, app_id, packet_hash, decision_hash,
			bundle_hash, risk, anchor_mode, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'pending')
		RETURNING id`,
		req.Packet.PacketID,
		req.Packet.CorrelationID,
		req.Packet.App.AppID,
		req.PacketHash,
		req.DecisionHash,
		req.BundleHash,
		string(req.Packet.Action.Risk),
		string(mode),
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("postgres: enqueue anchor %q: %w", req.Packet.PacketID, err)
	}
	return id, nil
}

// ClaimAnchorBatch atomically claims pending rows for the elected leader.
func (s *Store) ClaimAnchorBatch(ctx context.Context, limit int) ([]*ledger.StoredAnchorRequest, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		WITH claimed AS (
			SELECT id
			FROM anchor_queue
			WHERE status = 'pending'
			ORDER BY queued_at, id
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE anchor_queue q
		SET status = 'processing',
		    attempts = attempts + 1,
		    last_attempt_at = now(),
		    locked_at = now()
		FROM claimed
		WHERE q.id = claimed.id
		RETURNING q.id, q.packet_id, COALESCE(q.correlation_id,''), COALESCE(q.app_id,''),
		          q.packet_hash, q.decision_hash, q.bundle_hash, q.risk, q.anchor_mode`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: claim anchor batch: %w", err)
	}
	defer rows.Close()

	out := []*ledger.StoredAnchorRequest{}
	for rows.Next() {
		var row struct {
			id            int64
			packetID      string
			correlationID string
			appID         string
			packetHash    string
			decisionHash  string
			bundleHash    string
			risk          string
			anchorMode    string
		}
		if err := rows.Scan(
			&row.id,
			&row.packetID,
			&row.correlationID,
			&row.appID,
			&row.packetHash,
			&row.decisionHash,
			&row.bundleHash,
			&row.risk,
			&row.anchorMode,
		); err != nil {
			return nil, err
		}
		out = append(out, &ledger.StoredAnchorRequest{
			ID: row.id,
			AnchorRequest: &ledger.AnchorRequest{
				Packet: &core.Packet{
					SchemaVersion: core.SchemaVersion,
					PacketID:      row.packetID,
					CorrelationID: row.correlationID,
					App:           core.AppContext{AppID: row.appID},
					Action:        core.Action{Risk: core.RiskLevel(row.risk)},
					Ledger:        core.LedgerRecord{AnchorMode: core.AnchorMode(row.anchorMode)},
				},
				PacketHash:   row.packetHash,
				DecisionHash: row.decisionHash,
				BundleHash:   row.bundleHash,
			},
		})
	}
	return out, rows.Err()
}

// MarkAnchorAccepted records the receipt and marks the queue row anchored.
func (s *Store) MarkAnchorAccepted(ctx context.Context, id int64, r *core.Receipt) error {
	if r == nil {
		return fmt.Errorf("postgres: receipt required")
	}
	if r.ReceiptID == "" {
		r.ReceiptID = "rcpt_" + uuid.New().String()
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if err := insertReceiptTx(ctx, tx, r); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
		UPDATE anchor_queue
		SET status = 'anchored',
		    anchored_at = now(),
		    last_error = NULL
		WHERE id = $1`,
		id,
	)
	if err != nil {
		return fmt.Errorf("postgres: mark anchor accepted: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("postgres: anchor queue row %d not found", id)
	}
	return tx.Commit(ctx)
}

// MarkAnchorFailed releases the row for retry after a transient writer failure.
func (s *Store) MarkAnchorFailed(ctx context.Context, id int64, cause error) error {
	reason := ""
	if cause != nil {
		reason = strings.TrimSpace(cause.Error())
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE anchor_queue
		SET status = 'pending',
		    last_error = $2
		WHERE id = $1`,
		id,
		nullableString(reason),
	)
	if err != nil {
		return fmt.Errorf("postgres: mark anchor failed: %w", err)
	}
	return nil
}

// PendingAnchorDepth reports rows not yet anchored.
func (s *Store) PendingAnchorDepth(ctx context.Context) (int64, error) {
	var depth int64
	err := s.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM anchor_queue
		WHERE status IN ('pending','processing')`,
	).Scan(&depth)
	if err != nil {
		return 0, fmt.Errorf("postgres: anchor queue depth: %w", err)
	}
	return depth, nil
}

func insertReceiptTx(ctx context.Context, tx pgx.Tx, r *core.Receipt) error {
	_, err := tx.Exec(ctx, `
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
	if err != nil {
		return fmt.Errorf("postgres: insert receipt: %w", err)
	}
	return nil
}

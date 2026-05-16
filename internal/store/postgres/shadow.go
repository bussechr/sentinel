// Package postgres — shadow decision persistence and cold archive index.
//
// These helpers back the policy.ShadowSink and evidence cold archive
// retention paths. They are kept in a separate file because they belong
// to a later milestone than the original packet/decision/receipt set.
package postgres

import (
	"context"
	"time"

	"github.com/your-org/sentinel/internal/core"
	"github.com/your-org/sentinel/internal/policy"
)

// InsertShadowDecision stores a shadow vs active divergence record.
func (s *Store) InsertShadowDecision(ctx context.Context, rec *policy.ShadowDecisionRecord) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO shadow_decisions (
		    shadow_id, packet_id, correlation_id,
		    active_bundle_id, candidate_bundle_id, action_class,
		    active_decision, candidate_decision, diverged,
		    active_reason, candidate_reason, evaluated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (shadow_id) DO NOTHING`,
		rec.ShadowID, rec.PacketID, rec.CorrelationID,
		rec.ActiveBundleID, rec.CandidateBundleID, rec.ActionClass,
		string(rec.ActiveDecision), string(rec.CandidateDecision), rec.Diverged,
		rec.ActiveReason, rec.CandidateReason, rec.EvaluatedAt,
	)
	return err
}

// ListShadowDivergences returns recent divergence rows where the shadow
// decision differed from the active decision.
func (s *Store) ListShadowDivergences(ctx context.Context, since time.Time, limit int) ([]*policy.ShadowDecisionRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT shadow_id, packet_id, correlation_id,
		       active_bundle_id, candidate_bundle_id, COALESCE(action_class,''),
		       active_decision, candidate_decision, diverged,
		       COALESCE(active_reason,''), COALESCE(candidate_reason,''), evaluated_at
		FROM shadow_decisions
		WHERE diverged = TRUE AND evaluated_at >= $1
		ORDER BY evaluated_at DESC
		LIMIT $2`, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*policy.ShadowDecisionRecord
	for rows.Next() {
		var r policy.ShadowDecisionRecord
		var actDec, candDec string
		if err := rows.Scan(
			&r.ShadowID, &r.PacketID, &r.CorrelationID,
			&r.ActiveBundleID, &r.CandidateBundleID,
			&r.ActionClass,
			&actDec, &candDec, &r.Diverged,
			&r.ActiveReason, &r.CandidateReason, &r.EvaluatedAt,
		); err != nil {
			return nil, err
		}
		r.ActiveDecision = core.Decision(actDec)
		r.CandidateDecision = core.Decision(candDec)
		out = append(out, &r)
	}
	return out, rows.Err()
}

// ColdArchiveRecord is a pointer to evidence retired past the 72h window.
type ColdArchiveRecord struct {
	ArchiveID     string    `json:"archive_id"`
	CorrelationID string    `json:"correlation_id"`
	AppID         string    `json:"app_id"`
	ArchivedAt    time.Time `json:"archived_at"`
	ObjectURI     string    `json:"object_uri"`
	RecordCount   int       `json:"record_count"`
	BundleHash    string    `json:"bundle_hash,omitempty"`
	ProofLocator  string    `json:"proof_locator,omitempty"`
}

// InsertColdArchive records that a tranche of hot-window evidence has
// been moved to cold storage.
func (s *Store) InsertColdArchive(ctx context.Context, rec *ColdArchiveRecord) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO cold_archive_index (
		    archive_id, correlation_id, app_id, archived_at,
		    object_uri, record_count, bundle_hash, proof_locator)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		rec.ArchiveID, rec.CorrelationID, rec.AppID, rec.ArchivedAt,
		rec.ObjectURI, rec.RecordCount,
		nullableString(rec.BundleHash), nullableString(rec.ProofLocator),
	)
	return err
}

// LookupColdArchives returns archive pointers for a correlation ID.
func (s *Store) LookupColdArchives(ctx context.Context, correlationID string) ([]*ColdArchiveRecord, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT archive_id, correlation_id, app_id, archived_at,
		       object_uri, record_count, COALESCE(bundle_hash,''), COALESCE(proof_locator,'')
		FROM cold_archive_index
		WHERE correlation_id = $1
		ORDER BY archived_at DESC`, correlationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*ColdArchiveRecord
	for rows.Next() {
		var r ColdArchiveRecord
		if err := rows.Scan(
			&r.ArchiveID, &r.CorrelationID, &r.AppID, &r.ArchivedAt,
			&r.ObjectURI, &r.RecordCount, &r.BundleHash, &r.ProofLocator,
		); err != nil {
			return nil, err
		}
		out = append(out, &r)
	}
	return out, rows.Err()
}

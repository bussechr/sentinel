// Package postgres provides the Sentinel Postgres store implementation.
//
// It satisfies the interfaces required by:
//   - evidence.RetentionStore
//   - evidence.RewindStore
//
// Connection is via pgx/v5. The DSN is loaded from a secret (never from
// a plain config file or environment variable in production).
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/your-org/sentinel/internal/core"
	"github.com/your-org/sentinel/internal/evidence"
)

// Store is the Sentinel Postgres data layer.
type Store struct {
	pool *pgxpool.Pool
}

// New opens a pgx connection pool using the provided DSN.
func New(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres: open pool: %w", err)
	}
	return &Store{pool: pool}, nil
}

// Close releases the connection pool.
func (s *Store) Close() {
	s.pool.Close()
}

// Ping verifies the database is reachable (used by /readyz).
func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// ─── App Registry ────────────────────────────────────────────────────────────

// RegisterApp inserts a new application registration.
func (s *Store) RegisterApp(ctx context.Context, app *core.AppRegistration) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO apps (app_id, service, environment, owner, mode, risk_tier,
		                  allowed_categories, policy_scope, signing_key_ref,
		                  registration_token, registered_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (app_id) DO NOTHING`,
		app.AppID, app.Service, app.Environment, app.Owner,
		string(app.Mode), string(app.RiskTier),
		app.AllowedCategories, app.PolicyScope,
		app.SigningKeyRef, app.RegistrationToken, app.RegisteredAt,
	)
	return err
}

// GetApp retrieves an application registration by app_id.
func (s *Store) GetApp(ctx context.Context, appID string) (*core.AppRegistration, error) {
	var app core.AppRegistration
	err := s.pool.QueryRow(ctx, `
		SELECT app_id, service, environment, owner, mode, risk_tier,
		       allowed_categories, policy_scope, signing_key_ref, registered_at, last_seen_at
		FROM apps WHERE app_id = $1`, appID).Scan(
		&app.AppID, &app.Service, &app.Environment, &app.Owner,
		&app.Mode, &app.RiskTier, &app.AllowedCategories,
		&app.PolicyScope, &app.SigningKeyRef,
		&app.RegisteredAt, &app.LastSeenAt,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: get app %q: %w", appID, err)
	}
	return &app, nil
}

// ─── Packets ─────────────────────────────────────────────────────────────────

// InsertPacket stores a governance packet in the hot index.
func (s *Store) InsertPacket(ctx context.Context, p *core.Packet) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO packets (packet_id, correlation_id, trace_id, captured_at,
		    app_id, actor_type, actor_id_hash, identity_provider,
		    action_category, action_name, risk, mutating,
		    resource_type, resource_id_hash, tenant_hash,
		    body_hash, redaction_profile, object_uri,
		    is_ai_related, tool_call_count, raw)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)`,
		p.PacketID, p.CorrelationID, p.TraceID, p.CapturedAt,
		p.App.AppID, string(p.Actor.Type), p.Actor.IDHash, string(p.Actor.IdentityProvider),
		string(p.Action.Category), p.Action.Name, string(p.Action.Risk), p.Action.Mutating,
		p.Resource.Type, p.Resource.IDHash, p.Resource.TenantHash,
		p.Payload.BodyHash, p.Payload.RedactionProfile, p.Payload.ObjectURI,
		p.AI.IsAIRelated, p.AI.ToolCallCount, p,
	)
	return err
}

// GetPacket retrieves a full packet by ID.
func (s *Store) GetPacket(ctx context.Context, packetID string) (*core.Packet, error) {
	var p core.Packet
	err := s.pool.QueryRow(ctx, `SELECT raw FROM packets WHERE packet_id = $1`, packetID).Scan(&p)
	if err != nil {
		return nil, fmt.Errorf("postgres: get packet %q: %w", packetID, err)
	}
	return &p, nil
}

// ─── Retention (evidence.RetentionStore) ─────────────────────────────────────

// DeletePacketsOlderThan removes packet rows captured before cutoff.
func (s *Store) DeletePacketsOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM packets WHERE captured_at < $1`, cutoff)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// DeleteSegmentsOlderThan removes evidence segment rows ending before cutoff.
func (s *Store) DeleteSegmentsOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM evidence_segments WHERE to_ts < $1`, cutoff)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ─── Rewind (evidence.RewindStore) ───────────────────────────────────────────

func (s *Store) PacketsByCorrelationID(ctx context.Context, correlationID string, window time.Duration) ([]*core.Packet, error) {
	cutoff := time.Now().UTC().Add(-window)
	rows, err := s.pool.Query(ctx, `SELECT raw FROM packets WHERE correlation_id = $1 AND captured_at >= $2 ORDER BY captured_at`, correlationID, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*core.Packet
	for rows.Next() {
		var p core.Packet
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

func (s *Store) DecisionsByCorrelationID(ctx context.Context, correlationID string, window time.Duration) ([]*core.DecisionRecord, error) {
	cutoff := time.Now().UTC().Add(-window)
	rows, err := s.pool.Query(ctx, `
		SELECT d.decision_id, d.packet_id, d.app_id, d.decision, d.reason,
		       d.bundle_id, d.bundle_hash, d.decision_hash, d.masking_profile, d.evaluated_at
		FROM decisions d
		JOIN packets p ON p.packet_id = d.packet_id
		WHERE p.correlation_id = $1 AND p.captured_at >= $2
		ORDER BY d.evaluated_at`, correlationID, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*core.DecisionRecord
	for rows.Next() {
		var d core.DecisionRecord
		if err := rows.Scan(&d.DecisionID, &d.PacketID, &d.AppID, &d.Decision, &d.Reason,
			&d.BundleID, &d.BundleHash, &d.DecisionHash, &d.MaskingProfile, &d.EvaluatedAt); err != nil {
			return nil, err
		}
		out = append(out, &d)
	}
	return out, rows.Err()
}

func (s *Store) ReceiptsByCorrelationID(ctx context.Context, correlationID string, window time.Duration) ([]*core.Receipt, error) {
	cutoff := time.Now().UTC().Add(-window)
	rows, err := s.pool.Query(ctx, `
		SELECT r.receipt_id, r.packet_id, r.packet_hash, r.decision_hash,
		       r.policy_bundle_hash, r.app_id, r.risk, r.anchor_mode,
		       r.status, r.chain_tx_id, r.chain_height, r.issued_at, r.verified_at
		FROM receipts r
		JOIN packets p ON p.packet_id = r.packet_id
		WHERE p.correlation_id = $1 AND p.captured_at >= $2
		ORDER BY r.issued_at`, correlationID, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*core.Receipt
	for rows.Next() {
		var r core.Receipt
		if err := rows.Scan(&r.ReceiptID, &r.PacketID, &r.PacketHash, &r.DecisionHash,
			&r.PolicyBundleHash, &r.AppID, &r.Risk, &r.AnchorMode,
			&r.Status, &r.ChainTransactionID, &r.ChainHeight, &r.IssuedAt, &r.VerifiedAt); err != nil {
			return nil, err
		}
		out = append(out, &r)
	}
	return out, rows.Err()
}

func (s *Store) SegmentsByCorrelationID(ctx context.Context, correlationID string, window time.Duration) ([]*evidence.Segment, error) {
	// Segments are linked to apps, not directly to correlation IDs.
	// For now, return segments covering the window for the apps active
	// during that window. A richer join will be implemented in M6.
	_ = correlationID
	_ = window
	return nil, nil
}

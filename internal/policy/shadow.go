// Package policy — shadow bundle diffing.
//
// The Shadow engine evaluates each packet against an active OPA bundle
// and a candidate ("shadow") bundle in parallel. The active decision is
// authoritative; the shadow decision is persisted alongside it in
// shadow_decisions so operators can compare bundles before promotion.
//
// This is the operational complement to Engine.Simulate. Simulate runs
// once on demand; Shadow runs every authorise call and produces a
// continuous divergence record that drives confidence to promote.
package policy

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/your-org/sentinel/internal/core"
	"go.uber.org/zap"
)

// Shadow runs an active bundle (authoritative) and a candidate bundle
// (advisory) on the same packet and returns both decisions.
//
// The candidate is loaded lazily on first use so the active path is
// never blocked by a slow bundle fetch.
type Shadow struct {
	active            *Engine
	candidateURL      string
	candidateBundleID string
	mu                sync.RWMutex
	candidate         *Engine
	log               *zap.Logger
}

// NewShadow wraps an existing active engine and registers a candidate
// bundle URL for parallel evaluation.
//
// candidateBundleURL may be empty; in that case the shadow engine
// returns only the active result.
func NewShadow(active *Engine, candidateBundleURL, candidateBundleID string, log *zap.Logger) *Shadow {
	return &Shadow{
		active:            active,
		candidateURL:      candidateBundleURL,
		candidateBundleID: candidateBundleID,
		log:               log,
	}
}

// CandidateConfigured reports whether a shadow bundle is registered.
func (s *Shadow) CandidateConfigured() bool {
	if s == nil {
		return false
	}
	return s.candidateURL != ""
}

// ShadowResult is the dual outcome of one packet evaluation.
type ShadowResult struct {
	ShadowID          string          `json:"shadow_id"`
	Active            *EvaluateResult `json:"active"`
	Candidate         *EvaluateResult `json:"candidate,omitempty"`
	Diverged          bool            `json:"diverged"`
	ActiveBundleID    string          `json:"active_bundle_id"`
	CandidateBundleID string          `json:"candidate_bundle_id,omitempty"`
	EvaluatedAt       time.Time       `json:"evaluated_at"`
}

// Evaluate runs both engines concurrently. The active result is
// returned even if the candidate fails; candidate failure is logged
// rather than propagated.
func (s *Shadow) Evaluate(ctx context.Context, input *EvaluateInput) (*ShadowResult, error) {
	if s == nil || s.active == nil {
		return nil, fmt.Errorf("shadow: active engine not configured")
	}

	out := &ShadowResult{
		ShadowID:    "shd_" + uuid.New().String(),
		EvaluatedAt: time.Now().UTC(),
	}

	type res struct {
		r   *EvaluateResult
		err error
	}
	activeCh := make(chan res, 1)
	candCh := make(chan res, 1)

	go func() {
		r, err := s.active.Evaluate(ctx, input)
		activeCh <- res{r: r, err: err}
	}()

	if s.CandidateConfigured() {
		go func() {
			cand, err := s.loadCandidate(ctx)
			if err != nil {
				candCh <- res{err: err}
				return
			}
			r, err := cand.Evaluate(ctx, input)
			candCh <- res{r: r, err: err}
		}()
	} else {
		candCh <- res{}
	}

	a := <-activeCh
	if a.err != nil {
		return nil, a.err
	}
	out.Active = a.r
	out.ActiveBundleID = a.r.BundleID

	c := <-candCh
	if c.err != nil {
		s.log.Warn("shadow: candidate evaluation failed", zap.Error(c.err))
	} else if c.r != nil {
		out.Candidate = c.r
		out.CandidateBundleID = s.candidateBundleID
		out.Diverged = c.r.Decision != a.r.Decision
	}
	return out, nil
}

func (s *Shadow) loadCandidate(ctx context.Context) (*Engine, error) {
	s.mu.RLock()
	if s.candidate != nil {
		s.mu.RUnlock()
		return s.candidate, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.candidate != nil {
		return s.candidate, nil
	}
	cand, err := NewEngine(ctx, s.candidateURL, s.log)
	if err != nil {
		return nil, fmt.Errorf("shadow: load candidate %q: %w", s.candidateURL, err)
	}
	s.candidate = cand
	return cand, nil
}

// ShadowSink is the persistence interface for divergence records.
type ShadowSink interface {
	InsertShadowDecision(ctx context.Context, rec *ShadowDecisionRecord) error
}

// ShadowDecisionRecord is the persisted form of a ShadowResult, joined
// to packet identifiers so it can be queried by correlation_id.
type ShadowDecisionRecord struct {
	ShadowID          string        `json:"shadow_id" db:"shadow_id"`
	PacketID          string        `json:"packet_id" db:"packet_id"`
	CorrelationID     string        `json:"correlation_id" db:"correlation_id"`
	ActiveBundleID    string        `json:"active_bundle_id" db:"active_bundle_id"`
	CandidateBundleID string        `json:"candidate_bundle_id" db:"candidate_bundle_id"`
	ActionClass       string        `json:"action_class" db:"action_class"`
	ActiveDecision    core.Decision `json:"active_decision" db:"active_decision"`
	CandidateDecision core.Decision `json:"candidate_decision" db:"candidate_decision"`
	Diverged          bool          `json:"diverged" db:"diverged"`
	ActiveReason      string        `json:"active_reason" db:"active_reason"`
	CandidateReason   string        `json:"candidate_reason" db:"candidate_reason"`
	EvaluatedAt       time.Time     `json:"evaluated_at" db:"evaluated_at"`
}

// ToRecord converts a ShadowResult into a persistable record using the
// supplied packet identifiers.
func (r *ShadowResult) ToRecord(packetID, correlationID, actionClass string) *ShadowDecisionRecord {
	rec := &ShadowDecisionRecord{
		ShadowID:          r.ShadowID,
		PacketID:          packetID,
		CorrelationID:     correlationID,
		ActiveBundleID:    r.ActiveBundleID,
		CandidateBundleID: r.CandidateBundleID,
		ActionClass:       actionClass,
		Diverged:          r.Diverged,
		EvaluatedAt:       r.EvaluatedAt,
	}
	if r.Active != nil {
		rec.ActiveDecision = r.Active.Decision
		rec.ActiveReason = r.Active.Reason
	}
	if r.Candidate != nil {
		rec.CandidateDecision = r.Candidate.Decision
		rec.CandidateReason = r.Candidate.Reason
	}
	return rec
}

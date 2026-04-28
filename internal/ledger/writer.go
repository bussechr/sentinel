// Package ledger — writer abstraction.
//
// Writer is the unified interface that every chain or proof backend
// implements. The anchor queue holds one or more named writers and
// dispatches AnchorRequests by app- or risk-driven routing rule.
//
// Three reference backends are provided:
//   - CometBFT  : the default app chain (BFT state machine replication).
//   - Besu/QBFT : an EVM contract path for environments that need EVM semantics.
//   - immudb    : a verifiable proof backend for compact append-only receipts.
//
// Writers are stateless from the queue's perspective. Each Submit call
// returns a Receipt; LatestHeight and Health are used by the readiness probe.
package ledger

import (
	"context"
	"fmt"
	"sync"

	"github.com/your-org/sentinel/internal/core"
)

// WriterKind identifies a backend implementation. Used by the registry.
type WriterKind string

const (
	WriterCometBFT WriterKind = "cometbft"
	WriterBesu     WriterKind = "besu"
	WriterImmuDB   WriterKind = "immudb"
)

// Writer is the canonical chain or proof backend interface.
//
// Implementations must be safe for concurrent use by the anchor queue.
type Writer interface {
	Kind() WriterKind
	Name() string
	Submit(ctx context.Context, req *AnchorRequest) (*core.Receipt, error)
	Verify(ctx context.Context, receiptID string) (*core.Receipt, error)
	LatestHeight(ctx context.Context) (int64, error)
	Health(ctx context.Context) WriterHealth
}

// WriterHealth is the operational status of a writer.
type WriterHealth struct {
	Kind    WriterKind `json:"kind"`
	Name    string     `json:"name"`
	Healthy bool       `json:"healthy"`
	Height  int64      `json:"height"`
	Reason  string     `json:"reason,omitempty"`
}

// Registry holds named writers and a default writer.
//
// Routing is not coupled to the registry: a caller can pick a writer
// by name (e.g. by app risk profile) or fall back to the default.
type Registry struct {
	mu      sync.RWMutex
	writers map[string]Writer
	def     string
}

// NewRegistry returns an empty writer registry.
func NewRegistry() *Registry {
	return &Registry{writers: map[string]Writer{}}
}

// Register adds a writer under its Name(). The first writer registered
// becomes the default unless SetDefault is called explicitly.
func (r *Registry) Register(w Writer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := w.Name()
	r.writers[name] = w
	if r.def == "" {
		r.def = name
	}
}

// SetDefault marks a previously-registered writer as the default.
func (r *Registry) SetDefault(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.writers[name]; !ok {
		return fmt.Errorf("ledger: writer %q not registered", name)
	}
	r.def = name
	return nil
}

// Get returns a writer by name, or the default if name is empty.
func (r *Registry) Get(name string) (Writer, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if name == "" {
		name = r.def
	}
	if name == "" {
		return nil, fmt.Errorf("ledger: no writers registered")
	}
	w, ok := r.writers[name]
	if !ok {
		return nil, fmt.Errorf("ledger: writer %q not registered", name)
	}
	return w, nil
}

// Default returns the default writer, or nil if none is registered.
func (r *Registry) Default() Writer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.def == "" {
		return nil
	}
	return r.writers[r.def]
}

// Names returns the registered writer names in unspecified order.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.writers))
	for n := range r.writers {
		out = append(out, n)
	}
	return out
}

// HealthAll polls every writer and returns a per-writer health snapshot.
func (r *Registry) HealthAll(ctx context.Context) []WriterHealth {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]WriterHealth, 0, len(r.writers))
	for _, w := range r.writers {
		out = append(out, w.Health(ctx))
	}
	return out
}

// adaptBackend wraps a legacy Backend value as a Writer.
//
// The anchor queue's original Backend interface predates the Writer
// abstraction; this adapter lets the queue keep working unchanged
// while new code uses Writers directly.
type backendWriter struct {
	kind WriterKind
	name string
	be   Backend
}

// AsWriter converts a Backend into a Writer with the given kind/name.
func AsWriter(kind WriterKind, name string, be Backend) Writer {
	return &backendWriter{kind: kind, name: name, be: be}
}

func (b *backendWriter) Kind() WriterKind { return b.kind }
func (b *backendWriter) Name() string     { return b.name }

func (b *backendWriter) Submit(ctx context.Context, req *AnchorRequest) (*core.Receipt, error) {
	return b.be.Submit(ctx, req)
}

func (b *backendWriter) Verify(ctx context.Context, receiptID string) (*core.Receipt, error) {
	return b.be.Verify(ctx, receiptID)
}

func (b *backendWriter) LatestHeight(ctx context.Context) (int64, error) {
	return b.be.LatestHeight(ctx)
}

func (b *backendWriter) Health(ctx context.Context) WriterHealth {
	h := WriterHealth{Kind: b.kind, Name: b.name}
	height, err := b.be.LatestHeight(ctx)
	if err != nil {
		h.Reason = err.Error()
		return h
	}
	h.Height = height
	h.Healthy = true
	return h
}

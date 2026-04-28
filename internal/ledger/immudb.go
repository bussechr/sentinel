// Package ledger — immudb verifiable proof backend.
//
// immudb provides append-only cryptographic proofs (Merkle/SHA-256 inclusion
// proofs and consistency proofs across history). The Sentinel adapter writes
// a compact JSON proof document per packet (key=packet_id, value=document)
// using VerifiedSet, which returns a tamper-evident proof, and exposes
// Verify by re-reading the same key with VerifiedGet so the proof state
// is checked against the database root.
//
// When an endpoint is not configured, the backend keeps a deterministic
// in-memory shadow log so the queue, registry, and HTTP handler code
// remain exercisable in tests and demos. Receipt transaction IDs use
// the prefix `immudb:` either way.
package ledger

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	immuclient "github.com/codenotary/immudb/pkg/client"
	"github.com/google/uuid"
	"github.com/your-org/sentinel/internal/core"
	"go.uber.org/zap"
)

// ImmuDBBackend implements the Writer interface using an immudb-backed
// append-only verifiable KV store.
type ImmuDBBackend struct {
	endpoint string
	db       string
	username string
	password string
	name     string
	height   atomic.Int64
	log      *zap.Logger

	clientMu sync.Mutex
	client   immuclient.ImmuClient

	// In-memory shadow log used when no immudb endpoint is configured
	// (development, CI, demo). Production deployments must set endpoint.
	mu    sync.RWMutex
	store map[string]immuRecord
}

type immuRecord struct {
	Receipt *core.Receipt
	Hash    string
	Index   int64
}

// NewImmuDBBackend builds a verifiable proof backend.
//
// endpoint is host:port for the immudb server (e.g. "localhost:3322").
// db is the database name; the client logs in to that database before
// the first Submit. password is loaded from a mounted secret. When
// endpoint is empty, the backend operates against the in-memory shadow.
func NewImmuDBBackend(endpoint, db, username string, log *zap.Logger) *ImmuDBBackend {
	return &ImmuDBBackend{
		endpoint: endpoint,
		db:       db,
		username: username,
		name:     "immudb-default",
		log:      log,
		store:    make(map[string]immuRecord),
	}
}

// WithPassword sets the immudb login password (must be paired with a
// non-empty endpoint).
func (b *ImmuDBBackend) WithPassword(p string) *ImmuDBBackend {
	b.password = p
	return b
}

// Kind returns the writer kind identifier.
func (b *ImmuDBBackend) Kind() WriterKind { return WriterImmuDB }

// Name returns the registered writer name.
func (b *ImmuDBBackend) Name() string { return b.name }

// SetName overrides the default writer name.
func (b *ImmuDBBackend) SetName(n string) { b.name = n }

// Submit writes a canonical anchor document into immudb under the
// packet_id key using VerifiedSet. The returned proof's TxHeader id is
// recorded as the chain height. When no endpoint is configured the
// backend falls back to its in-memory shadow log.
func (b *ImmuDBBackend) Submit(ctx context.Context, req *AnchorRequest) (*core.Receipt, error) {
	doc := buildImmuDoc(req)
	body, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("immudb: marshal doc: %w", err)
	}
	sum := sha256.Sum256(body)
	txID := "immudb:" + hex.EncodeToString(sum[:])
	now := time.Now().UTC()

	if b.endpoint == "" {
		index := b.height.Add(1)
		receipt := b.makeReceipt(req, txID, index, core.AnchorAccepted, now)
		b.mu.Lock()
		b.store[req.Packet.PacketID] = immuRecord{Receipt: receipt, Hash: txID, Index: index}
		b.mu.Unlock()
		b.log.Info("immudb append (in-memory shadow)",
			zap.String("packet_id", req.Packet.PacketID),
			zap.String("tx_id", txID),
			zap.Int64("index", index),
		)
		return receipt, nil
	}

	c, err := b.connect(ctx)
	if err != nil {
		return nil, err
	}
	hdr, err := c.VerifiedSet(ctx, []byte(req.Packet.PacketID), body)
	if err != nil {
		return nil, fmt.Errorf("immudb: VerifiedSet: %w", err)
	}
	index := int64(hdr.Id)
	if h := b.height.Load(); index > h {
		b.height.Store(index)
	}
	receipt := b.makeReceipt(req, txID, index, core.AnchorAccepted, now)
	b.log.Info("immudb append committed",
		zap.String("endpoint", b.endpoint),
		zap.String("db", b.db),
		zap.String("packet_id", req.Packet.PacketID),
		zap.String("tx_id", txID),
		zap.Int64("tx_header_id", index),
	)
	return receipt, nil
}

// Verify reads the document back from immudb by packet ID using
// VerifiedGet so the proof state is checked against the current root.
// The receiptID is expected to embed the packet ID; for the in-memory
// shadow the receipt is found by ReceiptID.
func (b *ImmuDBBackend) Verify(ctx context.Context, receiptID string) (*core.Receipt, error) {
	if b.endpoint == "" {
		b.mu.RLock()
		defer b.mu.RUnlock()
		for _, rec := range b.store {
			if rec.Receipt != nil && rec.Receipt.ReceiptID == receiptID {
				return rec.Receipt, nil
			}
		}
		return nil, fmt.Errorf("immudb: receipt %q not found in shadow log", receiptID)
	}

	// In production the caller passes a packet_id-bearing key directly.
	c, err := b.connect(ctx)
	if err != nil {
		return nil, err
	}
	entry, err := c.VerifiedGet(ctx, []byte(receiptID))
	if err != nil {
		return nil, fmt.Errorf("immudb: VerifiedGet %q: %w", receiptID, err)
	}
	var doc immuDoc
	if err := json.Unmarshal(entry.Value, &doc); err != nil {
		return nil, fmt.Errorf("immudb: decode stored doc: %w", err)
	}
	return &core.Receipt{
		PacketID:           doc.PacketID,
		PacketHash:         doc.PacketHash,
		DecisionHash:       doc.DecisionHash,
		PolicyBundleHash:   doc.PolicyBundleHash,
		EvidenceRootHash:   doc.EvidenceRootHash,
		AppID:              doc.AppID,
		Risk:               doc.Risk,
		Status:             core.AnchorAnchored,
		WriterKind:         string(WriterImmuDB),
		WriterName:         b.name,
		ChainTransactionID: fmt.Sprintf("immudb:tx-%d-rev-%d", entry.Tx, entry.Revision),
		ChainHeight:        int64(entry.Revision),
		IssuedAt:           doc.At,
	}, nil
}

// LatestHeight returns the immudb state's TxId, or the in-memory counter
// when no endpoint is configured.
func (b *ImmuDBBackend) LatestHeight(ctx context.Context) (int64, error) {
	if b.endpoint == "" {
		return b.height.Load(), nil
	}
	c, err := b.connect(ctx)
	if err != nil {
		return 0, err
	}
	st, err := c.CurrentState(ctx)
	if err != nil {
		return 0, fmt.Errorf("immudb: CurrentState: %w", err)
	}
	return int64(st.TxId), nil
}

// Health reports backend status using LatestHeight as the liveness check.
func (b *ImmuDBBackend) Health(ctx context.Context) WriterHealth {
	h := WriterHealth{Kind: b.Kind(), Name: b.name}
	height, err := b.LatestHeight(ctx)
	if err != nil {
		h.Reason = err.Error()
		return h
	}
	h.Healthy = true
	h.Height = height
	return h
}

// connect lazily opens an immudb session and caches the client.
func (b *ImmuDBBackend) connect(ctx context.Context) (immuclient.ImmuClient, error) {
	b.clientMu.Lock()
	defer b.clientMu.Unlock()
	if b.client != nil {
		return b.client, nil
	}
	host, port := splitHostPort(b.endpoint)
	opts := immuclient.DefaultOptions().WithAddress(host).WithPort(port)
	c := immuclient.NewClient().WithOptions(opts)
	if err := c.OpenSession(ctx, []byte(b.username), []byte(b.password), b.db); err != nil {
		return nil, fmt.Errorf("immudb: open session: %w", err)
	}
	b.client = c
	return c, nil
}

// Close terminates the immudb session if one is open.
func (b *ImmuDBBackend) Close(ctx context.Context) error {
	b.clientMu.Lock()
	defer b.clientMu.Unlock()
	if b.client == nil {
		return nil
	}
	err := b.client.CloseSession(ctx)
	b.client = nil
	return err
}

// immuDoc is the canonical anchor record stored as the value bytes.
type immuDoc struct {
	Type             string         `json:"tx_type"`
	PacketID         string         `json:"packet_id"`
	PacketHash       string         `json:"packet_hash"`
	DecisionHash     string         `json:"decision_hash"`
	PolicyBundleHash string         `json:"policy_bundle_hash"`
	EvidenceRootHash string         `json:"evidence_root_hash,omitempty"`
	AppID            string         `json:"app_id"`
	Risk             core.RiskLevel `json:"risk"`
	At               time.Time      `json:"at"`
}

func buildImmuDoc(req *AnchorRequest) immuDoc {
	return immuDoc{
		Type:             "sentinel.anchor.immudb.v1",
		PacketID:         req.Packet.PacketID,
		PacketHash:       req.PacketHash,
		DecisionHash:     req.DecisionHash,
		PolicyBundleHash: req.BundleHash,
		AppID:            req.Packet.App.AppID,
		Risk:             req.Packet.Action.Risk,
		At:               time.Now().UTC(),
	}
}

func (b *ImmuDBBackend) makeReceipt(req *AnchorRequest, txID string, height int64, st core.AnchorStatus, at time.Time) *core.Receipt {
	return &core.Receipt{
		ReceiptID:          "rcpt_" + uuid.New().String(),
		PacketID:           req.Packet.PacketID,
		CorrelationID:      req.Packet.CorrelationID,
		PacketHash:         req.PacketHash,
		DecisionHash:       req.DecisionHash,
		PolicyBundleHash:   req.BundleHash,
		AppID:              req.Packet.App.AppID,
		Risk:               req.Packet.Action.Risk,
		AnchorMode:         req.Packet.Ledger.AnchorMode,
		Status:             st,
		WriterKind:         string(WriterImmuDB),
		WriterName:         b.name,
		ChainTransactionID: txID,
		ChainHeight:        height,
		IssuedAt:           at,
	}
}

// splitHostPort splits a host:port endpoint into its parts. Defaults
// the port to 3322 (immudb default) if absent.
func splitHostPort(endpoint string) (string, int) {
	host, port := endpoint, 3322
	for i := len(endpoint) - 1; i >= 0; i-- {
		if endpoint[i] == ':' {
			host = endpoint[:i]
			fmt.Sscanf(endpoint[i+1:], "%d", &port)
			break
		}
	}
	return host, port
}

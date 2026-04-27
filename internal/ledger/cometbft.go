// Package ledger — CometBFT chain backend.
//
// This adapter connects the anchor queue to a running CometBFT node.
// Only compact proof records are submitted (hashes, decision metadata,
// policy bundle hash, actor identity hash). Full payloads stay in object storage.
//
// CometBFT connection is via the RPC endpoint. In production, use the gRPC
// endpoint and mTLS. The adapter signs each transaction with the chain key,
// which is separate from the application signing keys.
package ledger

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/your-org/sentinel/internal/core"
	"go.uber.org/zap"
)

// AnchorTx is the chain transaction body (sentinel.anchor.v1).
// Keep this struct small — only hashes and metadata reach the chain.
type AnchorTx struct {
	TxType           string         `json:"tx_type"`
	PacketID         string         `json:"packet_id"`
	PacketHash       string         `json:"packet_hash"`
	DecisionHash     string         `json:"decision_hash"`
	PolicyBundleHash string         `json:"policy_bundle_hash"`
	AppID            string         `json:"app_id"`
	Risk             core.RiskLevel `json:"risk"`
	AnchorMode       core.AnchorMode `json:"anchor_mode"`
	Signature        string         `json:"signature"` // ed25519:<hex>
}

// CometBFTBackend implements the ledger.Backend interface against a CometBFT node.
type CometBFTBackend struct {
	rpcEndpoint string
	chainKey    []byte // ed25519 private key — loaded from secret, never from config file
	log         *zap.Logger
}

// NewCometBFTBackend creates a chain backend that submits to the given RPC endpoint.
// chainKey must be an ed25519 private key loaded from a mounted secret.
func NewCometBFTBackend(rpcEndpoint string, chainKey []byte, log *zap.Logger) *CometBFTBackend {
	return &CometBFTBackend{
		rpcEndpoint: rpcEndpoint,
		chainKey:    chainKey,
		log:         log,
	}
}

// Submit broadcasts a compact proof transaction to the CometBFT node.
func (b *CometBFTBackend) Submit(ctx context.Context, req *AnchorRequest) (*core.Receipt, error) {
	tx := AnchorTx{
		TxType:           "sentinel.anchor.v1",
		PacketID:         req.Packet.PacketID,
		PacketHash:       req.PacketHash,
		DecisionHash:     req.DecisionHash,
		PolicyBundleHash: req.BundleHash,
		AppID:            req.Packet.App.AppID,
		Risk:             req.Packet.Action.Risk,
		AnchorMode:       req.Packet.Ledger.AnchorMode,
		// TODO: sign with ed25519 chain key.
		Signature: "ed25519:placeholder",
	}

	txBytes, err := json.Marshal(tx)
	if err != nil {
		return nil, fmt.Errorf("cometbft: marshal tx: %w", err)
	}

	// TODO: replace with actual CometBFT RPC broadcast_tx_commit call.
	b.log.Info("CometBFT broadcast (stub)",
		zap.String("endpoint", b.rpcEndpoint),
		zap.String("packet_id", tx.PacketID),
		zap.Int("tx_bytes", len(txBytes)),
	)

	now := time.Now().UTC()
	receipt := &core.Receipt{
		ReceiptID:          "rcpt_" + uuid.New().String(),
		PacketID:           req.Packet.PacketID,
		PacketHash:         req.PacketHash,
		DecisionHash:       req.DecisionHash,
		PolicyBundleHash:   req.BundleHash,
		AppID:              req.Packet.App.AppID,
		Risk:               req.Packet.Action.Risk,
		AnchorMode:         req.Packet.Ledger.AnchorMode,
		Status:             core.AnchorAccepted,
		ChainTransactionID: "stub_tx_hash",
		IssuedAt:           now,
	}

	return receipt, nil
}

// Verify checks that a receipt's hashes still match what the chain reports.
func (b *CometBFTBackend) Verify(ctx context.Context, receiptID string) (*core.Receipt, error) {
	// TODO: query CometBFT for the transaction by packet hash and compare fields.
	b.log.Info("CometBFT verify (stub)", zap.String("receipt_id", receiptID))
	return nil, fmt.Errorf("cometbft: verify not yet implemented")
}

// LatestHeight returns the current chain block height for health checks.
func (b *CometBFTBackend) LatestHeight(ctx context.Context) (int64, error) {
	// TODO: call CometBFT /status endpoint.
	return 0, nil
}

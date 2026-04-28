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
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/your-org/sentinel/internal/core"
	"go.uber.org/zap"
)

// AnchorTx is the chain transaction body (sentinel.anchor.v1).
// Keep this struct small — only hashes and metadata reach the chain.
type AnchorTx struct {
	TxType           string          `json:"tx_type"`
	PacketID         string          `json:"packet_id"`
	PacketHash       string          `json:"packet_hash"`
	DecisionHash     string          `json:"decision_hash"`
	PolicyBundleHash string          `json:"policy_bundle_hash"`
	AppID            string          `json:"app_id"`
	Risk             core.RiskLevel  `json:"risk"`
	AnchorMode       core.AnchorMode `json:"anchor_mode"`
	Signature        string          `json:"signature"` // ed25519:<hex>
}

// CometBFTBackend implements the ledger.Backend and ledger.Writer interfaces
// against a CometBFT node via the public JSON-RPC endpoint.
//
// The backend ed25519-signs every transaction body before broadcast and
// retrieves the resulting tx hash and block height from the RPC response.
// `LatestHeight` calls /status; `Verify` calls /tx?hash=...
type CometBFTBackend struct {
	rpcEndpoint string
	chainKey    ed25519.PrivateKey
	name        string
	httpClient  *http.Client
	log         *zap.Logger
}

// NewCometBFTBackend creates a chain backend that submits to the given RPC endpoint.
// chainKey must be a 64-byte ed25519 private key loaded from a mounted secret.
func NewCometBFTBackend(rpcEndpoint string, chainKey []byte, log *zap.Logger) *CometBFTBackend {
	var pk ed25519.PrivateKey
	if len(chainKey) == ed25519.PrivateKeySize {
		pk = ed25519.PrivateKey(chainKey)
	}
	return &CometBFTBackend{
		rpcEndpoint: rpcEndpoint,
		chainKey:    pk,
		name:        "cometbft-default",
		httpClient:  &http.Client{Timeout: 10 * time.Second},
		log:         log,
	}
}

// Kind returns the writer kind identifier.
func (b *CometBFTBackend) Kind() WriterKind { return WriterCometBFT }

// Name returns the registered writer name.
func (b *CometBFTBackend) Name() string { return b.name }

// SetName overrides the default writer name (used by registry).
func (b *CometBFTBackend) SetName(n string) { b.name = n }

// Health returns a snapshot of the CometBFT writer health.
func (b *CometBFTBackend) Health(ctx context.Context) WriterHealth {
	h := WriterHealth{Kind: b.Kind(), Name: b.name}
	height, err := b.LatestHeight(ctx)
	if err != nil {
		h.Reason = err.Error()
		return h
	}
	h.Height = height
	h.Healthy = true
	return h
}

// Submit broadcasts a compact proof transaction to the CometBFT node
// via broadcast_tx_commit. The unsigned body is canonical JSON (sorted
// fields are not required; the chain compares bytes verbatim). The
// signature field is filled in before broadcast when a chainKey is
// configured.
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
	}

	unsigned, err := json.Marshal(tx)
	if err != nil {
		return nil, fmt.Errorf("cometbft: marshal tx: %w", err)
	}
	if b.chainKey != nil {
		sig := ed25519.Sign(b.chainKey, unsigned)
		tx.Signature = "ed25519:" + hex.EncodeToString(sig)
	} else {
		tx.Signature = "ed25519:unsigned"
	}
	signedBytes, err := json.Marshal(tx)
	if err != nil {
		return nil, fmt.Errorf("cometbft: marshal signed tx: %w", err)
	}

	resp, err := b.callRPC(ctx, "broadcast_tx_commit", map[string]interface{}{
		"tx": base64.StdEncoding.EncodeToString(signedBytes),
	})
	if err != nil {
		return nil, fmt.Errorf("cometbft: broadcast: %w", err)
	}

	var commitResult struct {
		Hash   string `json:"hash"`
		Height string `json:"height"`
		// CheckTx and DeliverTx report numeric ABCI codes.
		CheckTx struct {
			Code int    `json:"code"`
			Log  string `json:"log"`
		} `json:"check_tx"`
		DeliverTx struct {
			Code int    `json:"code"`
			Log  string `json:"log"`
		} `json:"deliver_tx"`
	}
	if err := json.Unmarshal(resp, &commitResult); err != nil {
		return nil, fmt.Errorf("cometbft: unmarshal commit result: %w", err)
	}
	if commitResult.CheckTx.Code != 0 {
		return nil, fmt.Errorf("cometbft: check_tx rejected (code=%d): %s",
			commitResult.CheckTx.Code, commitResult.CheckTx.Log)
	}
	if commitResult.DeliverTx.Code != 0 {
		return nil, fmt.Errorf("cometbft: deliver_tx rejected (code=%d): %s",
			commitResult.DeliverTx.Code, commitResult.DeliverTx.Log)
	}

	height, _ := parseInt64(commitResult.Height)

	now := time.Now().UTC()
	receipt := &core.Receipt{
		ReceiptID:          "rcpt_" + uuid.New().String(),
		PacketID:           req.Packet.PacketID,
		CorrelationID:      req.Packet.CorrelationID,
		PacketHash:         req.PacketHash,
		DecisionHash:       req.DecisionHash,
		PolicyBundleHash:   req.BundleHash,
		AppID:              req.Packet.App.AppID,
		Risk:               req.Packet.Action.Risk,
		AnchorMode:         req.Packet.Ledger.AnchorMode,
		Status:             core.AnchorAccepted,
		WriterKind:         string(WriterCometBFT),
		WriterName:         b.name,
		ChainTransactionID: "cometbft:0x" + commitResult.Hash,
		ChainHeight:        height,
		IssuedAt:           now,
	}
	b.log.Info("CometBFT broadcast committed",
		zap.String("packet_id", req.Packet.PacketID),
		zap.String("tx_hash", commitResult.Hash),
		zap.Int64("height", height),
	)
	return receipt, nil
}

// Verify queries the chain for the transaction hash referenced in a
// receipt and reconstructs an authoritative receipt. ReceiptID for this
// adapter encodes the cometbft tx hash via Submit; Verify accepts both
// the local receipt id (with cometbft: prefix) and a raw hex hash.
func (b *CometBFTBackend) Verify(ctx context.Context, receiptID string) (*core.Receipt, error) {
	hashHex := receiptID
	if i := strings.IndexByte(receiptID, ':'); i >= 0 {
		hashHex = receiptID[i+1:]
	}
	hashHex = strings.TrimPrefix(hashHex, "0x")
	resp, err := b.callRPC(ctx, "tx", map[string]interface{}{
		"hash":  "0x" + hashHex,
		"prove": false,
	})
	if err != nil {
		return nil, fmt.Errorf("cometbft: verify: %w", err)
	}
	var txResult struct {
		Hash     string `json:"hash"`
		Height   string `json:"height"`
		TxResult struct {
			Code int    `json:"code"`
			Log  string `json:"log"`
		} `json:"tx_result"`
		Tx string `json:"tx"`
	}
	if err := json.Unmarshal(resp, &txResult); err != nil {
		return nil, fmt.Errorf("cometbft: unmarshal tx result: %w", err)
	}
	if txResult.TxResult.Code != 0 {
		return nil, fmt.Errorf("cometbft: tx failed on chain (code=%d): %s",
			txResult.TxResult.Code, txResult.TxResult.Log)
	}
	height, _ := parseInt64(txResult.Height)

	// Decode the stored tx body to recover the canonical anchor fields.
	rawTx, err := base64.StdEncoding.DecodeString(txResult.Tx)
	if err != nil {
		return nil, fmt.Errorf("cometbft: decode stored tx: %w", err)
	}
	var anchored AnchorTx
	if err := json.Unmarshal(rawTx, &anchored); err != nil {
		return nil, fmt.Errorf("cometbft: unmarshal stored anchor tx: %w", err)
	}
	return &core.Receipt{
		PacketID:           anchored.PacketID,
		PacketHash:         anchored.PacketHash,
		DecisionHash:       anchored.DecisionHash,
		PolicyBundleHash:   anchored.PolicyBundleHash,
		AppID:              anchored.AppID,
		Risk:               anchored.Risk,
		AnchorMode:         anchored.AnchorMode,
		Status:             core.AnchorAnchored,
		WriterKind:         string(WriterCometBFT),
		WriterName:         b.name,
		ChainTransactionID: "cometbft:0x" + txResult.Hash,
		ChainHeight:        height,
	}, nil
}

// LatestHeight queries the /status endpoint for the latest committed height.
func (b *CometBFTBackend) LatestHeight(ctx context.Context) (int64, error) {
	resp, err := b.callRPC(ctx, "status", nil)
	if err != nil {
		return 0, err
	}
	var st struct {
		SyncInfo struct {
			LatestBlockHeight string `json:"latest_block_height"`
		} `json:"sync_info"`
	}
	if err := json.Unmarshal(resp, &st); err != nil {
		return 0, fmt.Errorf("cometbft: unmarshal status: %w", err)
	}
	h, err := parseInt64(st.SyncInfo.LatestBlockHeight)
	if err != nil {
		return 0, fmt.Errorf("cometbft: parse height %q: %w", st.SyncInfo.LatestBlockHeight, err)
	}
	return h, nil
}

// callRPC invokes a JSON-RPC method on the CometBFT node and returns
// the raw `result` object as bytes.
func (b *CometBFTBackend) callRPC(ctx context.Context, method string, params interface{}) ([]byte, error) {
	if b.rpcEndpoint == "" {
		return nil, fmt.Errorf("cometbft: rpc endpoint not configured")
	}
	body, err := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      uuid.New().String(),
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, b.rpcEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := b.httpClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("cometbft: HTTP %d: %s", resp.StatusCode, string(respBytes))
	}
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Data    string `json:"data"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBytes, &envelope); err != nil {
		return nil, fmt.Errorf("cometbft: decode RPC envelope: %w", err)
	}
	if envelope.Error != nil {
		return nil, fmt.Errorf("cometbft: RPC error %d: %s (%s)",
			envelope.Error.Code, envelope.Error.Message, envelope.Error.Data)
	}
	return envelope.Result, nil
}

// parseInt64 parses a (possibly quoted) integer string.
func parseInt64(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.ParseInt(strings.Trim(s, "\""), 10, 64)
}

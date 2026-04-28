// Package ledger — Besu/QBFT chain backend.
//
// Besu writes governance receipts via an EVM contract anchor registry.
// In production the writer holds an EVM RPC endpoint and a signer; the
// concrete RPC client and contract ABI calls are intentionally left as
// stubs that log and return a synthetic receipt so the higher layers
// can be exercised end-to-end without a running Besu node.
//
// The receipt's ChainTransactionID carries an `evm:` prefix so receipts
// can be distinguished from CometBFT receipts on inspection.
package ledger

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/your-org/sentinel/internal/core"
	"go.uber.org/zap"
)

// EVMSigner produces a signed raw transaction that calls anchor(bytes32,
// bytes32, bytes32) on the configured contract.
//
// Sentinel does not bundle an EVM signer; production wires this with
// go-ethereum, an HSM-backed signer, or a remote signer (web3signer,
// Fireblocks, Cubist). Tests can supply a stub signer that returns
// deterministic bytes to exercise the RPC plumbing.
type EVMSigner interface {
	// SignAnchorTx returns the 0x-prefixed hex-encoded raw transaction
	// for an anchor() call carrying the given payload digest.
	SignAnchorTx(ctx context.Context, contract, packetHash, decisionHash, bundleHash string) (rawTx string, err error)
}

// BesuBackend implements the Writer interface against a Besu/QBFT node.
type BesuBackend struct {
	rpcEndpoint     string
	contractAddress string
	signer          EVMSigner
	name            string
	httpClient      *http.Client
	height          atomic.Int64
	log             *zap.Logger
}

// NewBesuBackend builds a Besu writer.
//
// rpcEndpoint is the JSON-RPC URL (https or wss). contractAddress is the
// 0x-prefixed BlackBoxAnchorRegistry contract address. signerKey is the
// secp256k1 private key bytes loaded from a mounted secret; it is used
// only by the default in-process signer if you supply one. When nil,
// Submit returns a deterministic synthetic receipt so the queue and
// upstream code can be exercised without a chain.
func NewBesuBackend(rpcEndpoint, contractAddress string, signerKey []byte, log *zap.Logger) *BesuBackend {
	b := &BesuBackend{
		rpcEndpoint:     rpcEndpoint,
		contractAddress: contractAddress,
		name:            "besu-default",
		httpClient:      &http.Client{Timeout: 10 * time.Second},
		log:             log,
	}
	if len(signerKey) > 0 {
		b.signer = nil // production wires go-ethereum here; left for caller
	}
	return b
}

// WithSigner attaches a production EVM signer.
func (b *BesuBackend) WithSigner(s EVMSigner) *BesuBackend {
	b.signer = s
	return b
}

// Kind returns the writer kind identifier.
func (b *BesuBackend) Kind() WriterKind { return WriterBesu }

// Name returns the registered writer name.
func (b *BesuBackend) Name() string { return b.name }

// SetName overrides the default writer name.
func (b *BesuBackend) SetName(n string) { b.name = n }

// Submit either:
//   - signs and broadcasts an anchor() call via the configured EVMSigner
//     plus eth_sendRawTransaction, then waits briefly for inclusion via
//     eth_getTransactionReceipt; or
//   - if no signer is wired, computes a synthetic receipt so the queue
//     plumbing remains exercisable in tests and demos.
func (b *BesuBackend) Submit(ctx context.Context, req *AnchorRequest) (*core.Receipt, error) {
	if b.signer == nil {
		return b.submitSynthetic(req), nil
	}

	rawTx, err := b.signer.SignAnchorTx(ctx, b.contractAddress, req.PacketHash, req.DecisionHash, req.BundleHash)
	if err != nil {
		return nil, fmt.Errorf("besu: sign anchor tx: %w", err)
	}

	resp, err := b.callRPC(ctx, "eth_sendRawTransaction", []interface{}{rawTx})
	if err != nil {
		return nil, fmt.Errorf("besu: eth_sendRawTransaction: %w", err)
	}
	var txHash string
	if err := json.Unmarshal(resp, &txHash); err != nil {
		return nil, fmt.Errorf("besu: decode tx hash: %w", err)
	}

	height, _ := b.waitForReceipt(ctx, txHash)
	if h := b.height.Load(); height > h {
		b.height.Store(height)
	}

	now := time.Now().UTC()
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
		Status:             core.AnchorAccepted,
		WriterKind:         string(WriterBesu),
		WriterName:         b.name,
		ChainTransactionID: "evm:" + txHash,
		ChainHeight:        height,
		IssuedAt:           now,
	}, nil
}

func (b *BesuBackend) submitSynthetic(req *AnchorRequest) *core.Receipt {
	payload := struct {
		Type             string         `json:"tx_type"`
		PacketID         string         `json:"packet_id"`
		PacketHash       string         `json:"packet_hash"`
		DecisionHash     string         `json:"decision_hash"`
		PolicyBundleHash string         `json:"policy_bundle_hash"`
		AppID            string         `json:"app_id"`
		Risk             core.RiskLevel `json:"risk"`
	}{
		Type:             "sentinel.anchor.evm.v1",
		PacketID:         req.Packet.PacketID,
		PacketHash:       req.PacketHash,
		DecisionHash:     req.DecisionHash,
		PolicyBundleHash: req.BundleHash,
		AppID:            req.Packet.App.AppID,
		Risk:             req.Packet.Action.Risk,
	}
	body, _ := json.Marshal(payload)
	sum := sha256.Sum256(body)
	txHash := "evm:0x" + hex.EncodeToString(sum[:])
	height := b.height.Add(1)

	b.log.Info("Besu broadcast (synthetic, no signer wired)",
		zap.String("endpoint", b.rpcEndpoint),
		zap.String("contract", b.contractAddress),
		zap.String("packet_id", req.Packet.PacketID),
		zap.String("tx_hash", txHash),
	)

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
		Status:             core.AnchorAccepted,
		WriterKind:         string(WriterBesu),
		WriterName:         b.name,
		ChainTransactionID: txHash,
		ChainHeight:        height,
		IssuedAt:           time.Now().UTC(),
	}
}

// Verify resolves a receipt by its 0x-prefixed transaction hash via
// eth_getTransactionReceipt. Returns a Receipt with Status anchored
// when a block confirmation is present.
func (b *BesuBackend) Verify(ctx context.Context, receiptID string) (*core.Receipt, error) {
	hash := receiptID
	if i := strings.IndexByte(receiptID, ':'); i >= 0 {
		hash = receiptID[i+1:]
	}
	if !strings.HasPrefix(hash, "0x") {
		hash = "0x" + hash
	}
	if b.rpcEndpoint == "" {
		return nil, fmt.Errorf("besu: rpc endpoint not configured")
	}

	resp, err := b.callRPC(ctx, "eth_getTransactionReceipt", []interface{}{hash})
	if err != nil {
		return nil, err
	}
	if bytes.Equal(resp, []byte("null")) {
		return nil, fmt.Errorf("besu: transaction %s not yet mined", hash)
	}
	var r struct {
		BlockNumber string `json:"blockNumber"`
		Status      string `json:"status"`
		TxHash      string `json:"transactionHash"`
	}
	if err := json.Unmarshal(resp, &r); err != nil {
		return nil, fmt.Errorf("besu: decode receipt: %w", err)
	}
	height, _ := parseHexInt(r.BlockNumber)

	st := core.AnchorAnchored
	if r.Status != "" && r.Status != "0x1" {
		st = core.AnchorFailed
	}
	return &core.Receipt{
		Status:             st,
		WriterKind:         string(WriterBesu),
		WriterName:         b.name,
		ChainTransactionID: "evm:" + r.TxHash,
		ChainHeight:        height,
	}, nil
}

// LatestHeight calls eth_blockNumber. Falls back to the synthetic counter
// when the RPC endpoint is empty (test and CI default).
func (b *BesuBackend) LatestHeight(ctx context.Context) (int64, error) {
	if b.rpcEndpoint == "" {
		return b.height.Load(), nil
	}
	resp, err := b.callRPC(ctx, "eth_blockNumber", []interface{}{})
	if err != nil {
		return 0, err
	}
	var hex string
	if err := json.Unmarshal(resp, &hex); err != nil {
		return 0, fmt.Errorf("besu: decode blockNumber: %w", err)
	}
	return parseHexInt(hex)
}

// waitForReceipt polls eth_getTransactionReceipt up to 5 times with
// 500ms backoff. Returns the included block number, or 0 if not yet
// mined (Status will be queued in that case, not anchored).
func (b *BesuBackend) waitForReceipt(ctx context.Context, txHash string) (int64, error) {
	for attempt := 0; attempt < 5; attempt++ {
		resp, err := b.callRPC(ctx, "eth_getTransactionReceipt", []interface{}{txHash})
		if err == nil && !bytes.Equal(resp, []byte("null")) {
			var r struct {
				BlockNumber string `json:"blockNumber"`
			}
			if jsonErr := json.Unmarshal(resp, &r); jsonErr == nil && r.BlockNumber != "" {
				return parseHexInt(r.BlockNumber)
			}
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return 0, nil
}

// callRPC invokes a JSON-RPC method on the Besu node and returns the
// raw `result` field as bytes.
func (b *BesuBackend) callRPC(ctx context.Context, method string, params []interface{}) ([]byte, error) {
	if b.rpcEndpoint == "" {
		return nil, fmt.Errorf("besu: rpc endpoint not configured")
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
		return nil, fmt.Errorf("besu: HTTP %d: %s", resp.StatusCode, string(respBytes))
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
		return nil, fmt.Errorf("besu: decode RPC envelope: %w", err)
	}
	if envelope.Error != nil {
		return nil, fmt.Errorf("besu: RPC error %d: %s (%s)",
			envelope.Error.Code, envelope.Error.Message, envelope.Error.Data)
	}
	return envelope.Result, nil
}

// parseHexInt parses a 0x-prefixed hex string into an int64.
func parseHexInt(s string) (int64, error) {
	s = strings.TrimPrefix(s, "0x")
	if s == "" {
		return 0, nil
	}
	return strconv.ParseInt(s, 16, 64)
}

// Health returns a basic health snapshot derived from the synthetic counter.
func (b *BesuBackend) Health(ctx context.Context) WriterHealth {
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

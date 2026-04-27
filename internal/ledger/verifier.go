// Package ledger — receipt verifier.
//
// The verifier re-derives the packet hash and decision hash from stored data
// and checks that the chain receipt matches. It is the primary tool used by
// sentinelctl verify-ledger.
package ledger

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/your-org/sentinel/internal/core"
)

// Verifier checks that a stored receipt is consistent with the chain.
type Verifier struct {
	backend Backend
}

// NewVerifier creates a verifier backed by the given chain adapter.
func NewVerifier(backend Backend) *Verifier {
	return &Verifier{backend: backend}
}

// HashPacket computes the canonical SHA-256 hash of a packet.
// The hash is deterministic: packet_id + captured_at + action name + payload body hash.
func HashPacket(p *core.Packet) (string, error) {
	type canonical struct {
		PacketID  string `json:"packet_id"`
		CapturedAt string `json:"captured_at"`
		ActionName string `json:"action_name"`
		BodyHash  string `json:"body_hash"`
	}

	c := canonical{
		PacketID:  p.PacketID,
		CapturedAt: p.CapturedAt.UTC().Format("2006-01-02T15:04:05.999999999Z"),
		ActionName: p.Action.Name,
		BodyHash:  p.Payload.BodyHash,
	}

	b, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("verifier: marshal packet for hash: %w", err)
	}

	sum := sha256.Sum256(b)
	return fmt.Sprintf("sha256:%x", sum), nil
}

// VerifyResult is the output of a ledger verification check.
type VerifyResult struct {
	PacketID       string `json:"packet_id"`
	ReceiptID      string `json:"receipt_id"`
	LocalHash      string `json:"local_hash"`
	ChainHash      string `json:"chain_hash"`
	HashMatch      bool   `json:"hash_match"`
	ChainHeight    int64  `json:"chain_height"`
	Verified       bool   `json:"verified"`
	FailureReason  string `json:"failure_reason,omitempty"`
}

// Verify checks a receipt against the chain and the locally-computed packet hash.
func (v *Verifier) Verify(ctx context.Context, packet *core.Packet, receipt *core.Receipt) (*VerifyResult, error) {
	localHash, err := HashPacket(packet)
	if err != nil {
		return nil, err
	}

	chainReceipt, err := v.backend.Verify(ctx, receipt.ReceiptID)
	if err != nil {
		return &VerifyResult{
			PacketID:      packet.PacketID,
			ReceiptID:     receipt.ReceiptID,
			LocalHash:     localHash,
			Verified:      false,
			FailureReason: err.Error(),
		}, nil
	}

	height, _ := v.backend.LatestHeight(ctx)
	hashMatch := localHash == chainReceipt.PacketHash

	return &VerifyResult{
		PacketID:    packet.PacketID,
		ReceiptID:   receipt.ReceiptID,
		LocalHash:   localHash,
		ChainHash:   chainReceipt.PacketHash,
		HashMatch:   hashMatch,
		ChainHeight: height,
		Verified:    hashMatch,
	}, nil
}

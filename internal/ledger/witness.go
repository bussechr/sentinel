// Package ledger — local witness receipts.
//
// When the chain is unavailable or the packet risk does not require
// immediate chain anchoring, a local witness receipt is issued.
// The witness receipt is signed by the Sentinel API signing key and
// stored in Postgres. It provides durable evidence that the packet
// was accepted and evaluated — even before the chain confirms.
//
// Fail-closed behaviour: for high and critical risk with no chain path,
// the packet is rejected unless the app is in observe mode.
package ledger

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/your-org/sentinel/internal/core"
)

// WitnessReceipt is a locally-signed evidence record that a packet was evaluated.
type WitnessReceipt struct {
	WitnessID  string         `json:"witness_id"`
	PacketID   string         `json:"packet_id"`
	PacketHash string         `json:"packet_hash"`
	Decision   core.Decision  `json:"decision"`
	Risk       core.RiskLevel `json:"risk"`
	IssuedAt   time.Time      `json:"issued_at"`
	Signature  string         `json:"signature"`  // ed25519:<hex>
	PublicKey  string         `json:"public_key"` // hex
}

// Witness issues local evidence receipts using the Sentinel API signing key.
type Witness struct {
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
}

// NewWitness generates a new ephemeral witness key pair.
// In production, load the private key from a mounted secret.
func NewWitness() (*Witness, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("witness: generate key: %w", err)
	}
	return &Witness{privateKey: priv, publicKey: pub}, nil
}

// Issue creates and signs a witness receipt for the given packet hash.
func (w *Witness) Issue(packetID, packetHash string, decision core.Decision, risk core.RiskLevel) (*WitnessReceipt, error) {
	id := "wit_" + uuid.New().String()
	now := time.Now().UTC()

	msg := fmt.Sprintf("%s:%s:%s:%s:%s", id, packetID, packetHash, decision, now.Format(time.RFC3339Nano))
	sig := ed25519.Sign(w.privateKey, []byte(msg))

	return &WitnessReceipt{
		WitnessID:  id,
		PacketID:   packetID,
		PacketHash: packetHash,
		Decision:   decision,
		Risk:       risk,
		IssuedAt:   now,
		Signature:  "ed25519:" + hex.EncodeToString(sig),
		PublicKey:  hex.EncodeToString(w.publicKey),
	}, nil
}

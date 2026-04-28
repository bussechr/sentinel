// Package core — ledger receipt types.
//
// A Receipt is proof that a packet's hashes were accepted into the governance chain.
// The verifier compares the stored hashes against what the chain reports.
package core

import "time"

// Receipt is returned to the caller after a packet is anchored on the chain.
//
// Replay-friendly fields (EvidenceRootHash, CorrelationID, WriterKind)
// are inspired by the KYB-chain trust receipt: they let a verifier
// re-derive the proof and align it with the rewind index without
// re-fetching the full packet body.
type Receipt struct {
	ReceiptID          string       `json:"receipt_id" db:"receipt_id"`
	PacketID           string       `json:"packet_id" db:"packet_id"`
	CorrelationID      string       `json:"correlation_id,omitempty" db:"correlation_id"`
	PacketHash         string       `json:"packet_hash" db:"packet_hash"`
	DecisionHash       string       `json:"decision_hash" db:"decision_hash"`
	PolicyBundleHash   string       `json:"policy_bundle_hash" db:"policy_bundle_hash"`
	EvidenceRootHash   string       `json:"evidence_root_hash,omitempty" db:"evidence_root_hash"`
	AppID              string       `json:"app_id" db:"app_id"`
	Risk               RiskLevel    `json:"risk" db:"risk"`
	AnchorMode         AnchorMode   `json:"anchor_mode" db:"anchor_mode"`
	Status             AnchorStatus `json:"status" db:"status"`
	WriterKind         string       `json:"writer_kind,omitempty" db:"writer_kind"`
	WriterName         string       `json:"writer_name,omitempty" db:"writer_name"`
	ChainTransactionID string       `json:"chain_tx_id,omitempty" db:"chain_tx_id"`
	ChainHeight        int64        `json:"chain_height,omitempty" db:"chain_height"`
	IssuedAt           time.Time    `json:"issued_at" db:"issued_at"`
	VerifiedAt         *time.Time   `json:"verified_at,omitempty" db:"verified_at"`
}

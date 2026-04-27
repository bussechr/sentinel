// Package evidence manages the 72-hour rolling evidence window.
//
// Evidence is organised into segments. Each segment covers a time range,
// a set of packets, a record count, and a segment hash. The hash is stored
// in the chain for tamper detection; the full payload is in object storage.
//
// The 72-hour window is an operational limit. Evidence API calls refuse
// queries wider than 72 hours unless the caller uses export mode.
package evidence

import (
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const (
	// DefaultWindowDuration is the operational evidence window.
	DefaultWindowDuration = 72 * time.Hour
)

// Segment is a time-bounded, hashed evidence package.
type Segment struct {
	SegmentID        string    `json:"segment_id" db:"segment_id"`
	AppID            string    `json:"app_id" db:"app_id"`
	NodeID           string    `json:"node_id" db:"node_id"`
	FromTS           time.Time `json:"from_ts" db:"from_ts"`
	ToTS             time.Time `json:"to_ts" db:"to_ts"`
	RecordCount      int       `json:"record_count" db:"record_count"`
	SegmentHash      string    `json:"segment_hash" db:"segment_hash"`
	ObjectURI        string    `json:"object_uri" db:"object_uri"`
	RedactionProfile string    `json:"redaction_profile" db:"redaction_profile"`
	ChainAnchorID    string    `json:"chain_anchor_id,omitempty" db:"chain_anchor_id"`
	CollectorStatus  string    `json:"collector_status" db:"collector_status"` // ok | degraded | missing
}

// CollectorStatus values.
const (
	CollectorOK       = "ok"
	CollectorDegraded = "degraded"
	CollectorMissing  = "missing"
)

// NewSegment creates a Segment with a generated ID and hash.
// The hash is computed over appID + nodeID + fromTS + toTS + recordCount + objectURI.
func NewSegment(appID, nodeID, objectURI, redactionProfile string, from, to time.Time, recordCount int) *Segment {
	id := "seg_" + uuid.New().String()
	msg := fmt.Sprintf("%s:%s:%s:%s:%d:%s", appID, nodeID, from.UTC().Format(time.RFC3339Nano), to.UTC().Format(time.RFC3339Nano), recordCount, objectURI)
	sum := sha256.Sum256([]byte(msg))

	return &Segment{
		SegmentID:        id,
		AppID:            appID,
		NodeID:           nodeID,
		FromTS:           from,
		ToTS:             to,
		RecordCount:      recordCount,
		SegmentHash:      fmt.Sprintf("sha256:%x", sum),
		ObjectURI:        objectURI,
		RedactionProfile: redactionProfile,
		CollectorStatus:  CollectorOK,
	}
}

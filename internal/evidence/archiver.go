// Package evidence — hot to cold archiver.
//
// The Sentinel evidence layer keeps a 72-hour hot index in Postgres for
// operational rewind. Anything older must remain available for audit and
// replay, but does not need the same query latency.
//
// The Archiver runs before Retention. For each tranche of about-to-expire
// rows, it gathers packets and receipts by correlation_id, writes a
// canonical JSON manifest to object storage, and records a pointer in
// cold_archive_index so future rewind queries can resolve out-of-window
// correlation IDs.
//
// The archiver is intentionally idempotent: running it twice for the
// same correlation produces the same manifest content (modulo UUID).
// The retention step deletes hot rows only after archive succeeds.
package evidence

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/your-org/sentinel/internal/core"
	"go.uber.org/zap"
)

// ArchiveSink writes a manifest to durable object storage.
type ArchiveSink interface {
	PutManifest(ctx context.Context, key string, body []byte) (uri string, err error)
}

// ArchiveIndex records the pointer to a written manifest.
type ArchiveIndex interface {
	RecordArchive(ctx context.Context, rec *ArchiveRecord) error
}

// ArchiveSource yields the rows about to be retired.
type ArchiveSource interface {
	// CorrelationIDsBefore returns distinct correlation IDs whose latest
	// packet captured_at is older than cutoff.
	CorrelationIDsBefore(ctx context.Context, cutoff time.Time, limit int) ([]string, error)

	// HotEvidence loads all packets, decisions, and receipts for one
	// correlation ID still present in the hot index.
	HotEvidence(ctx context.Context, correlationID string) (*HotEvidence, error)
}

// HotEvidence is the union of records archiver writes for one correlation.
type HotEvidence struct {
	Packets   []*core.Packet         `json:"packets"`
	Decisions []*core.DecisionRecord `json:"decisions"`
	Receipts  []*core.Receipt        `json:"receipts"`
	Segments  []*Segment             `json:"segments,omitempty"`
}

// ArchiveRecord is the index entry pointing at a manifest in cold storage.
type ArchiveRecord struct {
	ArchiveID     string
	CorrelationID string
	AppID         string
	ArchivedAt    time.Time
	ObjectURI     string
	RecordCount   int
	BundleHash    string
	ProofLocator  string
}

// Archiver moves evidence from the hot index to durable cold storage.
type Archiver struct {
	source ArchiveSource
	sink   ArchiveSink
	index  ArchiveIndex
	window time.Duration
	log    *zap.Logger
}

// NewArchiver builds an archiver. window controls how old a correlation
// must be before archiving; if zero, DefaultWindowDuration is used.
func NewArchiver(source ArchiveSource, sink ArchiveSink, index ArchiveIndex, window time.Duration, log *zap.Logger) *Archiver {
	if window <= 0 {
		window = DefaultWindowDuration
	}
	return &Archiver{source: source, sink: sink, index: index, window: window, log: log}
}

// Run archives up to maxBatches correlation IDs that fall outside the
// hot window. Returns the number of correlations archived and any error.
func (a *Archiver) Run(ctx context.Context, maxBatches int) (int, error) {
	if a.source == nil || a.sink == nil || a.index == nil {
		return 0, fmt.Errorf("archiver: source, sink, and index are required")
	}
	if maxBatches <= 0 {
		maxBatches = 50
	}
	cutoff := time.Now().UTC().Add(-a.window)

	corrIDs, err := a.source.CorrelationIDsBefore(ctx, cutoff, maxBatches)
	if err != nil {
		return 0, fmt.Errorf("archiver: list correlations: %w", err)
	}

	var archived int
	for _, corrID := range corrIDs {
		if err := a.archiveOne(ctx, corrID); err != nil {
			a.log.Error("archive correlation failed",
				zap.String("correlation_id", corrID),
				zap.Error(err),
			)
			continue
		}
		archived++
	}
	a.log.Info("archive run complete",
		zap.Int("archived", archived),
		zap.Time("cutoff", cutoff),
	)
	return archived, nil
}

func (a *Archiver) archiveOne(ctx context.Context, correlationID string) error {
	hot, err := a.source.HotEvidence(ctx, correlationID)
	if err != nil {
		return fmt.Errorf("load hot evidence: %w", err)
	}
	if hot == nil || len(hot.Packets) == 0 {
		return nil
	}

	body, err := json.MarshalIndent(hot, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}

	archiveID := "arc_" + uuid.New().String()
	key := fmt.Sprintf("cold/%s/%s.json", correlationID, archiveID)
	uri, err := a.sink.PutManifest(ctx, key, body)
	if err != nil {
		return fmt.Errorf("put manifest: %w", err)
	}

	bundleHash := ""
	if len(hot.Decisions) > 0 {
		bundleHash = hot.Decisions[0].BundleHash
	}
	proofLocator := ""
	if len(hot.Receipts) > 0 {
		proofLocator = hot.Receipts[0].ChainTransactionID
	}

	rec := &ArchiveRecord{
		ArchiveID:     archiveID,
		CorrelationID: correlationID,
		AppID:         hot.Packets[0].App.AppID,
		ArchivedAt:    time.Now().UTC(),
		ObjectURI:     uri,
		RecordCount:   len(hot.Packets) + len(hot.Decisions) + len(hot.Receipts),
		BundleHash:    bundleHash,
		ProofLocator:  proofLocator,
	}
	if err := a.index.RecordArchive(ctx, rec); err != nil {
		return fmt.Errorf("record archive: %w", err)
	}
	a.log.Info("archived correlation to cold storage",
		zap.String("correlation_id", correlationID),
		zap.String("object_uri", uri),
	)
	return nil
}

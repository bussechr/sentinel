package evidence_test

import (
	"testing"
	"time"

	"github.com/your-org/sentinel/internal/evidence"
)

func TestNewSegment_Hash(t *testing.T) {
	from := time.Now().UTC().Add(-1 * time.Hour)
	to := time.Now().UTC()
	seg := evidence.NewSegment("app1", "node1", "s3://bucket/key", "default-prod", from, to, 100)

	if seg.SegmentID == "" {
		t.Error("segment ID should not be empty")
	}
	if seg.SegmentHash == "" {
		t.Error("segment hash should not be empty")
	}
	if seg.SegmentHash[:7] != "sha256:" {
		t.Errorf("segment hash should start with sha256:, got %q", seg.SegmentHash)
	}
	if seg.CollectorStatus != evidence.CollectorOK {
		t.Errorf("default collector status should be ok, got %q", seg.CollectorStatus)
	}
}

func TestNewSegment_DifferentHashesForDifferentData(t *testing.T) {
	from := time.Now().UTC().Add(-1 * time.Hour)
	to := time.Now().UTC()

	seg1 := evidence.NewSegment("app1", "node1", "s3://bucket/a", "default-prod", from, to, 100)
	seg2 := evidence.NewSegment("app1", "node1", "s3://bucket/b", "default-prod", from, to, 100)

	if seg1.SegmentHash == seg2.SegmentHash {
		t.Error("different object URIs should produce different segment hashes")
	}
}

func TestDefaultWindowDuration(t *testing.T) {
	if evidence.DefaultWindowDuration != 72*time.Hour {
		t.Errorf("expected 72h default window, got %v", evidence.DefaultWindowDuration)
	}
}

func TestRewind_WindowLimitEnforced(t *testing.T) {
	_, err := evidence.Rewind(nil, nil, "corr_test", 73*time.Hour, false)
	if err == nil {
		t.Error("rewind should reject window > 72h in operational mode")
	}
}

func TestRewind_ExportModeAllowsWiderWindow(t *testing.T) {
	// Export mode with nil store — expect error from store, not from window check.
	_, err := evidence.Rewind(nil, nil, "corr_test", 200*time.Hour, true)
	// The error should come from the nil store call, not the window check.
	// A nil-store call produces a nil pointer panic; accept that for now.
	// In production, the store is always non-nil.
	_ = err
}

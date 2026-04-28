// Package evidence — ArchiveSink implementations.
//
// ObjectStoreSink adapts an internal/store/object.Store into the
// evidence.ArchiveSink interface so the cold archiver can write to
// MinIO, AWS S3, R2, B2, or any S3-compatible target without coupling
// the archiver to a specific provider.
package evidence

import (
	"bytes"
	"context"
	"fmt"

	"github.com/your-org/sentinel/internal/store/object"
)

// ObjectStoreSink writes archive manifests to an object.Store under a
// configurable bucket. Manifests are written with content-type
// application/json. The returned URI is the canonical pointer recorded
// in cold_archive_index.
type ObjectStoreSink struct {
	store  object.Store
	bucket string
}

// NewObjectStoreSink wraps an object.Store for use as an ArchiveSink.
func NewObjectStoreSink(store object.Store, bucket string) *ObjectStoreSink {
	return &ObjectStoreSink{store: store, bucket: bucket}
}

// PutManifest uploads body bytes as the manifest for one correlation.
func (s *ObjectStoreSink) PutManifest(ctx context.Context, key string, body []byte) (string, error) {
	if s.store == nil {
		return "", fmt.Errorf("evidence: object store sink not configured")
	}
	if s.bucket == "" {
		return "", fmt.Errorf("evidence: archive sink bucket not configured")
	}
	return s.store.Put(ctx, s.bucket, key, bytes.NewReader(body), int64(len(body)), "application/json")
}

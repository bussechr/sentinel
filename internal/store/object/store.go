// Package object provides the object storage adapter for evidence payloads.
//
// Sentinel stores full redacted payloads and evidence bundles in object storage
// (MinIO for dev, S3-compatible for production). Only hashes are stored in Postgres.
//
// The Store interface is intentionally minimal — callers never reference
// MinIO or AWS SDK types directly.
package object

import (
	"context"
	"fmt"
	"io"
)

// Store is the object storage interface.
type Store interface {
	// Put uploads data to the given key and returns the canonical object URI.
	Put(ctx context.Context, bucket, key string, r io.Reader, size int64, contentType string) (string, error)
	// Get downloads an object by its URI.
	Get(ctx context.Context, uri string) (io.ReadCloser, error)
	// Exists returns true if the object URI exists.
	Exists(ctx context.Context, uri string) (bool, error)
}

// NopStore is a no-op implementation used when object storage is not configured.
// All writes succeed but data is discarded. Used in dev/test.
type NopStore struct{}

func (NopStore) Put(_ context.Context, bucket, key string, _ io.Reader, _ int64, _ string) (string, error) {
	return fmt.Sprintf("nop://%s/%s", bucket, key), nil
}
func (NopStore) Get(_ context.Context, uri string) (io.ReadCloser, error) {
	return nil, fmt.Errorf("nop store: get not supported: %s", uri)
}
func (NopStore) Exists(_ context.Context, _ string) (bool, error) { return false, nil }

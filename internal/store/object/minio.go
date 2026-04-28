// Package object — MinIO/S3-compatible adapter.
//
// MinioStore implements the object.Store interface against any
// S3-compatible endpoint (AWS S3, MinIO, Cloudflare R2, Backblaze B2,
// Wasabi). Object URIs are returned as `s3://<bucket>/<key>` so the
// scheme is portable across providers.
package object

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// MinioStore is an S3-compatible object.Store.
type MinioStore struct {
	client *minio.Client
}

// MinioConfig holds the connection parameters for MinioStore.
type MinioConfig struct {
	Endpoint        string // host:port (no scheme)
	AccessKeyID     string
	SecretAccessKey string
	UseSSL          bool
	Region          string
	InsecureTLS     bool // skip TLS verification (only for dev MinIO)
}

// NewMinio opens an S3-compatible client.
func NewMinio(cfg MinioConfig) (*MinioStore, error) {
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("object: minio endpoint required")
	}
	transport := http.DefaultTransport
	if cfg.InsecureTLS && cfg.UseSSL {
		transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}
	c, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:     credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure:    cfg.UseSSL,
		Region:    cfg.Region,
		Transport: transport,
	})
	if err != nil {
		return nil, fmt.Errorf("object: minio client: %w", err)
	}
	return &MinioStore{client: c}, nil
}

// EnsureBucket creates the bucket if it does not yet exist.
func (s *MinioStore) EnsureBucket(ctx context.Context, bucket, region string) error {
	exists, err := s.client.BucketExists(ctx, bucket)
	if err != nil {
		return fmt.Errorf("object: bucket exists check: %w", err)
	}
	if exists {
		return nil
	}
	return s.client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: region})
}

// Put uploads bytes and returns an s3://bucket/key URI.
func (s *MinioStore) Put(ctx context.Context, bucket, key string, r io.Reader, size int64, contentType string) (string, error) {
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	_, err := s.client.PutObject(ctx, bucket, key, r, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return "", fmt.Errorf("object: put %s/%s: %w", bucket, key, err)
	}
	return fmt.Sprintf("s3://%s/%s", bucket, key), nil
}

// Get downloads an object by its s3:// URI.
func (s *MinioStore) Get(ctx context.Context, uri string) (io.ReadCloser, error) {
	bucket, key, err := parseS3URI(uri)
	if err != nil {
		return nil, err
	}
	obj, err := s.client.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("object: get %s: %w", uri, err)
	}
	return obj, nil
}

// Exists reports whether the object URI is present in the bucket.
func (s *MinioStore) Exists(ctx context.Context, uri string) (bool, error) {
	bucket, key, err := parseS3URI(uri)
	if err != nil {
		return false, err
	}
	_, err = s.client.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
	if err != nil {
		errResp := minio.ToErrorResponse(err)
		if errResp.Code == "NoSuchKey" || errResp.Code == "NotFound" {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// parseS3URI splits an s3://bucket/key URI.
func parseS3URI(uri string) (string, string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", "", fmt.Errorf("object: invalid uri %q: %w", uri, err)
	}
	if u.Scheme != "s3" {
		return "", "", fmt.Errorf("object: scheme must be s3, got %q", u.Scheme)
	}
	bucket := u.Host
	key := strings.TrimPrefix(u.Path, "/")
	if bucket == "" || key == "" {
		return "", "", fmt.Errorf("object: uri missing bucket or key: %q", uri)
	}
	return bucket, key, nil
}

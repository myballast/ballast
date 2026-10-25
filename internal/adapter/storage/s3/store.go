package s3

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// MaxObjectBytes bounds what is written and what is read back in one piece.
// Artifacts are held in memory, so the limit is also the most one request can
// ask of the process.
const MaxObjectBytes = 64 << 20

var (
	// ErrNotFound is a key with nothing under it.
	ErrNotFound = errors.New("object not found")
	// ErrTooLarge is an object over MaxObjectBytes.
	ErrTooLarge = errors.New("object is too large")
)

// Config says how to reach the store.
type Config struct {
	// Endpoint is host:port, with no scheme.
	Endpoint string
	Bucket   string
	// AccessKey and SecretKey are the application's own, limited to Bucket.
	AccessKey string
	SecretKey string
	UseSSL    bool
}

// Store reads and writes the objects of one bucket.
type Store struct {
	client *minio.Client
	bucket string
}

// New prepares a store. It does not connect: whether the server answers is the
// first call's business, so a process can start while the store is restarting.
func New(cfg Config) (*Store, error) {
	if cfg.Endpoint == "" || cfg.Bucket == "" || cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, errors.New("the object store needs an endpoint, a bucket and credentials")
	}

	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("prepare the object store client: %w", err)
	}

	return &Store{client: client, bucket: cfg.Bucket}, nil
}

// EnsureBucket creates the bucket if it is not there. Running it again is
// harmless.
//
// Creating a bucket is an administrator's right: the application's own
// credentials, limited to the bucket, cannot. It is for a development store and
// for whoever sets a new one up — never something a running service does.
func (s *Store) EnsureBucket(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("look for the bucket: %w", err)
	}
	if exists {
		return nil
	}

	if err := s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{}); err != nil {
		// Two callers can both see it missing; the second one's refusal is a
		// bucket that exists.
		if minio.ToErrorResponse(err).Code == "BucketAlreadyOwnedByYou" {
			return nil
		}
		return fmt.Errorf("create the bucket: %w", err)
	}
	return nil
}

// Put writes an object, replacing what is under the key.
func (s *Store) Put(ctx context.Context, key string, data []byte, contentType string) error {
	if key == "" {
		return errors.New("an object needs a key")
	}
	if len(data) > MaxObjectBytes {
		return ErrTooLarge
	}

	_, err := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("write the object: %w", err)
	}
	return nil
}

// Get reads an object. A key with nothing under it is ErrNotFound.
func (s *Store) Get(ctx context.Context, key string) ([]byte, error) {
	if key == "" {
		return nil, errors.New("an object needs a key")
	}

	object, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("read the object: %w", err)
	}
	defer func() { _ = object.Close() }()

	// One more byte than the limit tells an object that fits from one that
	// does not, without reading the rest of it.
	data, err := io.ReadAll(io.LimitReader(object, MaxObjectBytes+1))
	if err != nil {
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("read the object: %w", err)
	}
	if len(data) > MaxObjectBytes {
		return nil, ErrTooLarge
	}
	return data, nil
}

// Delete removes an object. Removing one that is not there is not an error, as
// in S3.
func (s *Store) Delete(ctx context.Context, key string) error {
	if key == "" {
		return errors.New("an object needs a key")
	}

	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("remove the object: %w", err)
	}
	return nil
}

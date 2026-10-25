//go:build integration

package s3_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/youssefmahersi/ballast/internal/adapter/storage/s3"
)

// The development service of compose.yaml, with its development credentials.
// The root user is used here because creating a bucket is a root right: the
// application's own credentials cannot do it, by design.
func config(t *testing.T) s3.Config {
	t.Helper()

	endpoint := os.Getenv("BALLAST_S3_TEST_ENDPOINT")
	if endpoint == "" {
		endpoint = "127.0.0.1:9000"
	}

	raw := make([]byte, 4)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}

	return s3.Config{
		Endpoint:  endpoint,
		Bucket:    "ballast-test-" + hex.EncodeToString(raw),
		AccessKey: "ballast",
		SecretKey: "ballastballast",
	}
}

// waitUntilReady blocks until the server answers: the image has no healthcheck,
// so `docker compose up --wait` returns when the process has started, not when
// it serves.
func waitUntilReady(t *testing.T, endpoint string) {
	t.Helper()

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		res, err := http.Get("http://" + endpoint + "/minio/health/ready")
		if err == nil {
			_ = res.Body.Close()
			if res.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("the object store at %s is not answering; is `make up` running?", endpoint)
}

// removeBucket leaves the development store as it found it. It lives here, with
// the client the store hides, because nothing in the application ever deletes a
// bucket and nothing should be able to.
func removeBucket(ctx context.Context, cfg s3.Config) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds: credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
	})
	if err != nil {
		return
	}
	for object := range client.ListObjects(ctx, cfg.Bucket, minio.ListObjectsOptions{Recursive: true}) {
		_ = client.RemoveObject(ctx, cfg.Bucket, object.Key, minio.RemoveObjectOptions{})
	}
	_ = client.RemoveBucket(ctx, cfg.Bucket)
}

func newStore(t *testing.T) (*s3.Store, context.Context) {
	t.Helper()

	cfg := config(t)
	waitUntilReady(t, cfg.Endpoint)

	store, err := s3.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)

	if err := store.EnsureBucket(ctx); err != nil {
		t.Fatalf("EnsureBucket: %v", err)
	}
	t.Cleanup(func() { removeBucket(context.WithoutCancel(ctx), cfg) })

	return store, ctx
}

func TestEnsureBucketCreatesItOnceAndIsHarmlessTheSecondTime(t *testing.T) {
	store, ctx := newStore(t)

	if err := store.EnsureBucket(ctx); err != nil {
		t.Errorf("a second EnsureBucket failed: %v", err)
	}
}

func TestWhatIsWrittenIsReadBack(t *testing.T) {
	store, ctx := newStore(t)

	want := []byte(`{"metrics":{"http_reqs":{"count":42}}}`)
	key := "org/11111111-1111-1111-1111-111111111111/run/22222222-2222-2222-2222-222222222222/summary.json"

	if err := store.Put(ctx, key, want, "application/json"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("read back %q, wrote %q", got, want)
	}
}

func TestAMissingObjectIsNotFoundAndNothingElse(t *testing.T) {
	store, ctx := newStore(t)

	_, err := store.Get(ctx, "org/none/run/none/summary.json")
	if !errors.Is(err, s3.ErrNotFound) {
		t.Errorf("Get of a missing key = %v, want ErrNotFound", err)
	}
}

func TestADeletedObjectIsGoneAndDeletingTwiceIsFine(t *testing.T) {
	store, ctx := newStore(t)

	const key = "org/a/run/b/summary.json"
	if err := store.Put(ctx, key, []byte("x"), "text/plain"); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Get(ctx, key); !errors.Is(err, s3.ErrNotFound) {
		t.Errorf("the object is still there: %v", err)
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Errorf("deleting a missing object failed: %v", err)
	}
}

func TestOneBucketKeepsTwoOrganizationsApartByTheirPrefix(t *testing.T) {
	store, ctx := newStore(t)

	a := "org/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa/run/r/summary.json"
	b := "org/bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb/run/r/summary.json"
	if err := store.Put(ctx, a, []byte("A"), "text/plain"); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, b, []byte("B"), "text/plain"); err != nil {
		t.Fatal(err)
	}

	gotA, _ := store.Get(ctx, a)
	gotB, _ := store.Get(ctx, b)
	if string(gotA) != "A" || string(gotB) != "B" {
		t.Errorf("the same run under two organizations collided: %q, %q", gotA, gotB)
	}
}

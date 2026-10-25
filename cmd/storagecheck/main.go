// Command storagecheck proves the object store works from where it runs: it
// writes a small object, reads it back, compares, and removes it.
//
// It is how a deployment shows that the store is reachable with the
// application's own credentials, which is more than the store being up: the
// credentials, the endpoint and the bucket all have to be right. Exit 0 and the
// store works; anything else, and the log says which step did not.
//
// With -create-bucket it makes the bucket first. That is for a development
// store: the application's credentials cannot create one, by design.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/adapter/storage/s3"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/infra/config"
	"github.com/youssefmahersi/ballast/internal/infra/logging"
)

func main() {
	createBucket := flag.Bool("create-bucket", false, "create the bucket first (development stores only)")
	flag.Parse()

	log := logging.New("storagecheck")

	if err := check(log, *createBucket); err != nil {
		log.Error("the object store check failed", "error", err)
		os.Exit(1)
	}
}

func check(log *slog.Logger, createBucket bool) error {
	cfg, err := config.LoadStorage()
	if err != nil {
		return err
	}

	store, err := s3.New(s3.Config{
		Endpoint:  cfg.Endpoint,
		Bucket:    cfg.Bucket,
		AccessKey: cfg.AccessKey,
		SecretKey: cfg.SecretKey,
		UseSSL:    cfg.UseSSL,
	})
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if createBucket {
		if err := store.EnsureBucket(ctx); err != nil {
			return err
		}
		log.Info("the bucket exists", "bucket", cfg.Bucket)
	}

	// Random identifiers: nothing here is a real organization or a real run, and
	// nobody's objects can be in the way.
	key, err := domain.ArtifactKey(uuid.New(), uuid.New(), "storage-check.json")
	if err != nil {
		return err
	}
	written := fmt.Appendf(nil, `{"check":"ballast storage","at":%q}`, time.Now().UTC().Format(time.RFC3339Nano))

	if err := store.Put(ctx, key, written, "application/json"); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	log.Info("written", "endpoint", cfg.Endpoint, "bucket", cfg.Bucket, "key", key, "bytes", len(written))

	// Whatever happens below, do not leave the object behind.
	defer func() {
		if err := store.Delete(context.WithoutCancel(ctx), key); err != nil {
			log.Warn("the check object could not be removed", "key", key, "error", err)
		}
	}()

	read, err := store.Get(ctx, key)
	if err != nil {
		return fmt.Errorf("read back: %w", err)
	}
	if !bytes.Equal(read, written) {
		return errors.New("read back something other than what was written")
	}
	log.Info("read back and identical", "key", key)

	if err := store.Delete(ctx, key); err != nil {
		return fmt.Errorf("remove: %w", err)
	}
	if _, err := store.Get(ctx, key); !errors.Is(err, s3.ErrNotFound) {
		return fmt.Errorf("the object is still there after its removal (%v)", err)
	}
	log.Info("removed")

	return nil
}

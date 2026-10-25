package s3

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// New does not connect: nothing below needs a server.
func newOffline(t *testing.T) *Store {
	t.Helper()

	store, err := New(Config{
		Endpoint: "127.0.0.1:1", Bucket: "ballast", AccessKey: "key", SecretKey: "never-printed-secret",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return store
}

func TestNewRefusesAnIncompleteConfiguration(t *testing.T) {
	complete := Config{Endpoint: "127.0.0.1:9000", Bucket: "ballast", AccessKey: "key", SecretKey: "secret"}

	for name, mutate := range map[string]func(*Config){
		"no endpoint":   func(c *Config) { c.Endpoint = "" },
		"no bucket":     func(c *Config) { c.Bucket = "" },
		"no access key": func(c *Config) { c.AccessKey = "" },
		"no secret key": func(c *Config) { c.SecretKey = "" },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := complete
			mutate(&cfg)
			if _, err := New(cfg); err == nil {
				t.Error("New accepted an incomplete configuration")
			}
		})
	}
}

func TestAnObjectOverTheLimitIsRefusedBeforeAnythingIsSent(t *testing.T) {
	store := newOffline(t)

	err := store.Put(context.Background(), "org/x/run/y/big.json", make([]byte, MaxObjectBytes+1), "application/json")
	if !errors.Is(err, ErrTooLarge) {
		t.Errorf("Put of an oversize object = %v, want ErrTooLarge", err)
	}
}

func TestAnEmptyKeyIsRefused(t *testing.T) {
	store := newOffline(t)

	if err := store.Put(context.Background(), "", []byte("x"), "text/plain"); err == nil {
		t.Error("Put accepted an empty key")
	}
	if _, err := store.Get(context.Background(), ""); err == nil {
		t.Error("Get accepted an empty key")
	}
	if err := store.Delete(context.Background(), ""); err == nil {
		t.Error("Delete accepted an empty key")
	}
}

func TestAConnectionErrorNeverRepeatsTheSecretKey(t *testing.T) {
	store := newOffline(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3_000_000_000)
	defer cancel()

	for name, err := range map[string]error{
		"Put":          store.Put(ctx, "k", []byte("x"), "text/plain"),
		"EnsureBucket": store.EnsureBucket(ctx),
	} {
		if err == nil {
			t.Fatalf("%s reached a server that is not there", name)
		}
		if strings.Contains(err.Error(), "never-printed-secret") {
			t.Errorf("%s repeats the secret key: %v", name, err)
		}
	}
}

package config

import (
	"fmt"
	"strconv"
	"strings"
)

// Storage is how to reach the object store.
type Storage struct {
	// Endpoint is host:port, with no scheme: the scheme is UseSSL's business.
	Endpoint string
	// Bucket is the one bucket of the application. Organizations are kept apart
	// by the prefix of their keys, not by bucket (see domain.ArtifactKey).
	Bucket string
	// AccessKey and SecretKey are the application's own, limited to Bucket.
	// Never the store's administrator.
	AccessKey string
	SecretKey string
	// UseSSL is on unless it is switched off: the store may be across the
	// internet, and a private network is the exception that says so.
	UseSSL bool
}

// LoadStorage reads the object store's configuration. Nothing has a default
// but TLS: a fallback to a local store is a process that starts in production
// and writes its artifacts nowhere.
func LoadStorage() (Storage, error) {
	cfg := Storage{
		Endpoint:  env("BALLAST_S3_ENDPOINT", ""),
		Bucket:    env("BALLAST_S3_BUCKET", ""),
		AccessKey: env("BALLAST_S3_ACCESS_KEY", ""),
		SecretKey: env("BALLAST_S3_SECRET_KEY", ""),
		UseSSL:    true,
	}

	for key, value := range map[string]string{
		"BALLAST_S3_ENDPOINT":   cfg.Endpoint,
		"BALLAST_S3_BUCKET":     cfg.Bucket,
		"BALLAST_S3_ACCESS_KEY": cfg.AccessKey,
		"BALLAST_S3_SECRET_KEY": cfg.SecretKey,
	} {
		if value == "" {
			return Storage{}, fmt.Errorf("%s is not set", key)
		}
	}

	if strings.ContainsAny(cfg.Endpoint, "/ ") {
		return Storage{}, fmt.Errorf("BALLAST_S3_ENDPOINT is host:port, with no scheme and no path")
	}

	if raw := env("BALLAST_S3_USE_SSL", ""); raw != "" {
		useSSL, err := strconv.ParseBool(raw)
		if err != nil {
			return Storage{}, fmt.Errorf("BALLAST_S3_USE_SSL is not true or false")
		}
		cfg.UseSSL = useSSL
	}

	return cfg, nil
}

// String leaves the secret key out: this struct ends up in a log line the day
// someone prints "the configuration" to find out what a process read.
func (s Storage) String() string {
	return fmt.Sprintf("storage{endpoint=%s bucket=%s access_key=%s secret_key=*** ssl=%t}",
		s.Endpoint, s.Bucket, s.AccessKey, s.UseSSL)
}

// GoString covers %#v, which does not go through String.
func (s Storage) GoString() string { return s.String() }

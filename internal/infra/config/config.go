package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// Config is everything the API and the worker read from their environment.
type Config struct {
	// DatabaseURL must point at the application role, which is neither a
	// superuser nor BYPASSRLS and owns no table. Connected as anything else,
	// the row-level security policies are decoration.
	DatabaseURL string
	// Addr is where the HTTP API listens.
	Addr string
	// DevOrgID is the organization every request is attributed to until
	// authentication exists. It is a placeholder for a session, not a design.
	DevOrgID uuid.UUID
	// K6Image is the generator image, pinned.
	K6Image string
	// RunTTL is the hard ceiling on a generator's life.
	RunTTL time.Duration
	// PollInterval is how long the worker waits when it found nothing to do.
	PollInterval time.Duration
	// ExtraHosts is passed to the generator's container, in host:ip form. It
	// exists so a development run can reach a server on the host machine.
	ExtraHosts []string
}

// Load reads the configuration, refusing to start rather than guessing at
// anything whose absence would be silently wrong.
func Load() (Config, error) {
	cfg := Config{
		DatabaseURL:  env("BALLAST_DATABASE_URL", "postgres://ballast_app:ballast_app@127.0.0.1:5432/ballast?sslmode=disable"),
		Addr:         env("BALLAST_ADDR", ":8080"),
		K6Image:      env("BALLAST_K6_IMAGE", ""),
		PollInterval: time.Second,
	}

	rawOrg := env("BALLAST_DEV_ORG_ID", "")
	if rawOrg == "" {
		return Config{}, fmt.Errorf("BALLAST_DEV_ORG_ID is not set; run `make seed` and export the organization it prints")
	}

	orgID, err := uuid.Parse(rawOrg)
	if err != nil {
		return Config{}, fmt.Errorf("BALLAST_DEV_ORG_ID is not a uuid: %w", err)
	}
	cfg.DevOrgID = orgID

	ttl, err := duration("BALLAST_RUN_TTL", 15*time.Minute)
	if err != nil {
		return Config{}, err
	}
	cfg.RunTTL = ttl

	poll, err := duration("BALLAST_POLL_INTERVAL", time.Second)
	if err != nil {
		return Config{}, err
	}
	cfg.PollInterval = poll

	if host := env("BALLAST_EXTRA_HOST", ""); host != "" {
		cfg.ExtraHosts = []string{host}
	}

	return cfg, nil
}

func env(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}
	return fallback
}

func duration(key string, fallback time.Duration) (time.Duration, error) {
	raw := env(key, "")
	if raw == "" {
		return fallback, nil
	}

	// Plain seconds are accepted too, because that is how the same limits are
	// written everywhere else in this project.
	if seconds, err := strconv.Atoi(raw); err == nil {
		return time.Duration(seconds) * time.Second, nil
	}

	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s is not a duration: %w", key, err)
	}
	return parsed, nil
}

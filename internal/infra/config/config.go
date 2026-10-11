package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/youssefmahersi/ballast/internal/infra/crypto"
	"github.com/youssefmahersi/ballast/internal/infra/netguard"
)

// Config is everything the API and the worker read from their environment.
type Config struct {
	// DatabaseURL must point at the application role, which is neither a
	// superuser nor BYPASSRLS and owns no table. Connected as anything else,
	// the row-level security policies are decoration.
	DatabaseURL string
	// Addr is where the HTTP API listens.
	Addr string
	// K6Image is the generator image, pinned.
	K6Image string
	// RunTTL is the hard ceiling on a generator's life.
	RunTTL time.Duration
	// PollInterval is how long the worker waits when it found nothing to do.
	PollInterval time.Duration
	// CollectInterval is how often the worker looks for runs whose own worker
	// has died. Shorter than the staleness threshold it enforces, so that a run
	// is not left abandoned for the two put together.
	CollectInterval time.Duration
	// ExtraHosts is passed to the generator's container, in host:ip form. It
	// exists so a development run can reach a server on the host machine.
	ExtraHosts []string
	// SecretKey encrypts the secrets of an environment at rest. Both processes
	// need it: the API writes them, the worker hands them to the generator.
	SecretKey []byte
	// EgressAllowedCIDRs names networks the egress proxy may reach although
	// they are not publicly routable, in CIDR form.
	//
	// **Empty in production.** It exists so that a run launched on a
	// developer's machine can reach a target on that machine, whose address is
	// private. Naming a range here lets a generator — running a user's script —
	// reach it.
	EgressAllowedCIDRs []string
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

	collect, err := duration("BALLAST_COLLECT_INTERVAL", 30*time.Second)
	if err != nil {
		return Config{}, err
	}
	cfg.CollectInterval = collect

	if host := env("BALLAST_EXTRA_HOST", ""); host != "" {
		cfg.ExtraHosts = []string{host}
	}

	cfg.EgressAllowedCIDRs = list(env("BALLAST_EGRESS_ALLOWED_CIDRS", ""))
	// Read here rather than left to the proxy, so a typo stops the worker
	// instead of every run it takes.
	if _, err := netguard.ParsePrefixes(cfg.EgressAllowedCIDRs); err != nil {
		return Config{}, fmt.Errorf("BALLAST_EGRESS_ALLOWED_CIDRS: %w", err)
	}

	// No default and no starting without it. A process that came up with no key
	// would run until somebody set their first secret, and then fail somewhere
	// far from the cause; worse, a default would be a key in the repository, used
	// by whoever never read this far.
	raw := env("BALLAST_SECRET_KEY", "")
	if raw == "" {
		return Config{}, errors.New(
			"BALLAST_SECRET_KEY is not set; it encrypts the secrets of an environment at rest. Generate one with: openssl rand -base64 32")
	}
	key, err := crypto.KeyFromBase64(raw)
	if err != nil {
		return Config{}, fmt.Errorf("BALLAST_SECRET_KEY: %w. Generate one with: openssl rand -base64 32", err)
	}
	cfg.SecretKey = key

	return cfg, nil
}

// list reads a comma-separated setting.
func list(raw string) []string {
	var entries []string
	for _, entry := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(entry); trimmed != "" {
			entries = append(entries, trimmed)
		}
	}
	return entries
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

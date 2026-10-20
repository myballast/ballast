package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/youssefmahersi/ballast/internal/domain"
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
	// AcceptingRuns is the platform's switch. Set to false, no new run is
	// taken: the API refuses one and the worker leaves the queue alone. Runs
	// already under way are not touched — stopping those is what the emergency
	// stop is for, and restarting a process must never be a way of killing a
	// run that is measuring something.
	AcceptingRuns bool
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
	// No default. A fallback to a local database is a process that starts in
	// production, answers its health check, and fails on every request that
	// touches a row — with nothing in the startup log to say why. The one
	// machine where the local address is right is a developer's, and the
	// Makefile sets it there.
	database := env("BALLAST_DATABASE_URL", "")
	if database == "" {
		return Config{}, fmt.Errorf(
			"BALLAST_DATABASE_URL is not set; it points at the application role. " +
				"Locally: postgres://ballast_app:ballast_app@127.0.0.1:5432/ballast?sslmode=disable")
	}

	cfg := Config{
		DatabaseURL:  database,
		Addr:         env("BALLAST_ADDR", ":8080"),
		K6Image:      env("BALLAST_K6_IMAGE", ""),
		PollInterval: time.Second,
	}

	// The hard ceiling plus five minutes: the container is killed only after
	// the moment a run that reached the ceiling should have ended. The same
	// five minutes the environment lock allows for starting a container and
	// reading a summary.
	ttl, err := duration("BALLAST_RUN_TTL", domain.MaxDurationAnywhere+5*time.Minute)
	if err != nil {
		return Config{}, err
	}
	// Three values say how long a run may last: the environment's ceiling, the
	// one no configuration can raise, and this time limit, which the generator
	// carries inside its own container. The first two are checked when a run
	// is accepted; this one kills the container whatever anybody decided.
	//
	// A time limit below the hard ceiling would mean a run this installation
	// accepts is a run the generator is killed in the middle of — reported as
	// a timeout, with nothing saying why. Refusing to start is the same answer
	// this file already gives to a missing key: a configuration that cannot
	// work is said at startup rather than discovered at three in the morning.
	if ttl < domain.MaxDurationAnywhere {
		return Config{}, fmt.Errorf(
			"BALLAST_RUN_TTL is %s, under the %s a run may last; a run would be killed mid-measurement",
			ttl, domain.MaxDurationAnywhere)
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

	// On unless somebody said otherwise, and read the same way by both
	// processes: an installation where the API refuses runs while the worker
	// keeps starting queued ones would be the worst of both.
	accepting, err := boolean("BALLAST_ACCEPTING_RUNS", true)
	if err != nil {
		return Config{}, err
	}
	cfg.AcceptingRuns = accepting

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

// boolean reads a switch, refusing a value it cannot read rather than guessing.
//
// A typo in the variable that stops an installation from taking runs must not
// quietly mean "on": somebody throwing that switch during an incident has to be
// told their spelling was wrong, by a process that refuses to start.
func boolean(key string, fallback bool) (bool, error) {
	raw := env(key, "")
	if raw == "" {
		return fallback, nil
	}

	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s is not a boolean: %q", key, raw)
	}
	return parsed, nil
}

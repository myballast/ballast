package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/infra/config"
)

// A key of the right length, so that reading the configuration fails for the
// reason a test is about rather than for a missing secret.
const aKey = "YmFsbGFzdC1kZXZlbG9wbWVudC1rZXktZG8tbm90LXU="

// An address of the right shape, for the same reason.
const aDSN = "postgres://ballast_app:ballast_app@127.0.0.1:5432/ballast?sslmode=disable"

// required sets what every process must have, so that a test fails for the
// setting it is about and not for one it did not mention.
func required(t *testing.T) {
	t.Helper()
	t.Setenv("BALLAST_SECRET_KEY", aKey)
	t.Setenv("BALLAST_DATABASE_URL", aDSN)
}

// A process that falls back to a local database starts in production, answers
// its health check, and fails on every request that reads a row. The refusal
// names the variable, because a message that does not is a message somebody
// has to go and look up.
func TestAMissingDatabaseAddressStopsTheProcess(t *testing.T) {
	t.Setenv("BALLAST_SECRET_KEY", aKey)
	t.Setenv("BALLAST_DATABASE_URL", "")

	_, err := config.Load()
	if err == nil {
		t.Fatal("a configuration with no database address was accepted")
	}
	if !strings.Contains(err.Error(), "BALLAST_DATABASE_URL") {
		t.Fatalf("the refusal does not name the setting: %v", err)
	}
}

// Three values say how long a run may last, and two of them can contradict
// each other in silence. A generator killed in the middle of a run it was
// allowed to start is reported as a timeout, with nothing saying why — so the
// process refuses to start instead.
func TestATimeLimitUnderTheHardCeilingStopsTheProcess(t *testing.T) {
	required(t)
	t.Setenv("BALLAST_RUN_TTL", "15m")

	_, err := config.Load()
	if err == nil {
		t.Fatal("a time limit under the ceiling was accepted")
	}
	if !strings.Contains(err.Error(), "BALLAST_RUN_TTL") {
		t.Fatalf("the refusal does not name the setting: %v", err)
	}
}

// At the ceiling exactly is allowed: a run that reaches it ends as the
// generator is killed, which is late enough.
func TestATimeLimitAtTheCeilingIsAccepted(t *testing.T) {
	required(t)
	t.Setenv("BALLAST_RUN_TTL", domain.MaxDurationAnywhere.String())

	if _, err := config.Load(); err != nil {
		t.Fatalf("a time limit at the ceiling was refused: %v", err)
	}
}

// And the default clears it on its own, so an installation that sets nothing
// starts and runs the longest run it will accept.
func TestTheDefaultTimeLimitClearsTheCeiling(t *testing.T) {
	required(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("the default configuration was refused: %v", err)
	}

	if cfg.RunTTL < domain.MaxDurationAnywhere {
		t.Fatalf("the default time limit is %s, under the %s ceiling",
			cfg.RunTTL, domain.MaxDurationAnywhere)
	}
	// And not absurdly above it either: the time limit is what stops a
	// runaway container, so it is the ceiling plus a margin, not a day.
	if cfg.RunTTL > domain.MaxDurationAnywhere+10*time.Minute {
		t.Fatalf("the default time limit is %s, far above the ceiling", cfg.RunTTL)
	}
}

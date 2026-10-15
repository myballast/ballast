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

// Three values say how long a run may last, and two of them can contradict
// each other in silence. A generator killed in the middle of a run it was
// allowed to start is reported as a timeout, with nothing saying why — so the
// process refuses to start instead.
func TestATimeLimitUnderTheHardCeilingStopsTheProcess(t *testing.T) {
	t.Setenv("BALLAST_SECRET_KEY", aKey)
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
	t.Setenv("BALLAST_SECRET_KEY", aKey)
	t.Setenv("BALLAST_RUN_TTL", domain.MaxDurationAnywhere.String())

	if _, err := config.Load(); err != nil {
		t.Fatalf("a time limit at the ceiling was refused: %v", err)
	}
}

// And the default clears it on its own, so an installation that sets nothing
// starts and runs the longest run it will accept.
func TestTheDefaultTimeLimitClearsTheCeiling(t *testing.T) {
	t.Setenv("BALLAST_SECRET_KEY", aKey)

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

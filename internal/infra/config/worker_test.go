package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/youssefmahersi/ballast/internal/infra/config"
)

const anOrg = "00000000-0000-4000-8000-000000000001"

func requiredWorker(t *testing.T) {
	t.Helper()
	required(t)
	t.Setenv("BALLAST_WORKER_ORG_ID", anOrg)
}

// Draining waits for the run that is executing, and this is how long it waits
// before giving up and stopping it anyway. The run cannot outlive its own time
// limit, so a wait that ends before that is a deployment that kills runs a
// generator was still allowed to finish — the same silent contradiction the time
// limit itself is guarded against, and answered the same way.
func TestADrainShorterThanARunStopsTheProcess(t *testing.T) {
	requiredWorker(t)
	t.Setenv("BALLAST_RUN_TTL", "1h5m")
	t.Setenv("BALLAST_DRAIN_TIMEOUT", "10m")

	_, err := config.LoadWorker()
	if err == nil {
		t.Fatal("a drain shorter than a run was accepted: a deployment would kill measurements")
	}
	if !strings.Contains(err.Error(), "BALLAST_DRAIN_TIMEOUT") {
		t.Fatalf("the refusal does not name the setting: %v", err)
	}
}

func TestADrainAsLongAsARunIsAccepted(t *testing.T) {
	requiredWorker(t)
	t.Setenv("BALLAST_RUN_TTL", "1h5m")
	t.Setenv("BALLAST_DRAIN_TIMEOUT", "1h5m")

	if _, err := config.LoadWorker(); err != nil {
		t.Fatalf("a drain as long as a run was refused: %v", err)
	}
}

// An installation that sets nothing still never kills a run by stopping.
func TestTheDefaultDrainOutlastsARun(t *testing.T) {
	requiredWorker(t)

	cfg, err := config.LoadWorker()
	if err != nil {
		t.Fatalf("the default configuration was refused: %v", err)
	}

	if cfg.DrainTimeout < cfg.RunTTL {
		t.Fatalf("the default drain is %s, under the %s a run may last", cfg.DrainTimeout, cfg.RunTTL)
	}
	if cfg.DrainTimeout > cfg.RunTTL+10*time.Minute {
		t.Fatalf("the default drain is %s, far above the %s a run may last", cfg.DrainTimeout, cfg.RunTTL)
	}
}

// The name is what makes a restarted worker the same runner and not a second one
// beside a dead row.
func TestTheRunnerNameIsReadFromTheEnvironment(t *testing.T) {
	requiredWorker(t)
	t.Setenv("BALLAST_RUNNER_NAME", "vps-1")

	cfg, err := config.LoadWorker()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.RunnerName != "vps-1" {
		t.Fatalf("runner name = %q, want vps-1", cfg.RunnerName)
	}
}

// Left unset, it is the machine's own name: one worker per machine is the
// arrangement, and the machine already has a name nobody has to invent.
func TestTheRunnerNameDefaultsToTheHostName(t *testing.T) {
	requiredWorker(t)

	cfg, err := config.LoadWorker()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.RunnerName == "" {
		t.Fatal("no default runner name")
	}
}

// The directory a run's files are made in is named to Docker, which resolves it
// on the host. A worker in a container therefore has to be told one that exists
// at the same path on both sides; left alone it would use its own /tmp, which the
// host has never heard of, and a run would start without its script.
func TestTheWorkDirectoryIsReadFromTheEnvironment(t *testing.T) {
	requiredWorker(t)
	t.Setenv("BALLAST_WORK_DIR", "/var/lib/ballast/runs")

	cfg, err := config.LoadWorker()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.WorkDir != "/var/lib/ballast/runs" {
		t.Fatalf("work dir = %q", cfg.WorkDir)
	}
}

// Left unset it stays empty, which is the operating system's temporary folder:
// right for a worker on a developer's machine, where there is no second side.
func TestTheWorkDirectoryIsEmptyByDefault(t *testing.T) {
	requiredWorker(t)

	cfg, err := config.LoadWorker()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.WorkDir != "" {
		t.Fatalf("work dir = %q, want none", cfg.WorkDir)
	}
}

// A path that is not absolute would be resolved by the daemon against its own
// directory, not this one's.
func TestARelativeWorkDirectoryStopsTheProcess(t *testing.T) {
	requiredWorker(t)
	t.Setenv("BALLAST_WORK_DIR", "runs")

	_, err := config.LoadWorker()
	if err == nil {
		t.Fatal("a relative work directory was accepted")
	}
	if !strings.Contains(err.Error(), "BALLAST_WORK_DIR") {
		t.Fatalf("the refusal does not name the setting: %v", err)
	}
}

// The proxy image is named by its build, like the worker's: a machine that runs
// the proxy of another commit than the worker's is a machine nobody can reason
// about.
func TestTheProxyImageIsReadFromTheEnvironment(t *testing.T) {
	requiredWorker(t)
	t.Setenv("BALLAST_PROXY_IMAGE", "ballast/egress-proxy:a1b2c3d")

	cfg, err := config.LoadWorker()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.ProxyImage != "ballast/egress-proxy:a1b2c3d" {
		t.Fatalf("proxy image = %q", cfg.ProxyImage)
	}
}

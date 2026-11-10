package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

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

// A worker with no organization named serves all of them. This is the line that
// used to be an error, and what the multi-organization worker is: the variable
// that used to be required is now a restriction.
func TestAWorkerWithNoOrganizationServesEveryOne(t *testing.T) {
	required(t)

	cfg, err := config.LoadWorker()
	if err != nil {
		t.Fatalf("a worker with no organization named was refused: %v", err)
	}
	if cfg.OrgID != uuid.Nil {
		t.Fatalf("org id = %s, want none (every organization)", cfg.OrgID)
	}
}

// Named, it is what it always was: a worker for that organization only.
func TestAWorkerWithAnOrganizationNamedIsRestrictedToIt(t *testing.T) {
	requiredWorker(t)

	cfg, err := config.LoadWorker()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.OrgID.String() != anOrg {
		t.Fatalf("org id = %s, want %s", cfg.OrgID, anOrg)
	}
}

// A typo is not "every organization": a worker that failed to read the one it was
// told to restrict itself to and quietly served all of them would be the opposite
// of what whoever wrote the variable meant.
func TestAnOrganizationThatIsNotAUUIDStopsTheProcess(t *testing.T) {
	required(t)
	t.Setenv("BALLAST_WORKER_ORG_ID", "not-a-uuid")

	_, err := config.LoadWorker()
	if err == nil {
		t.Fatal("an unreadable organization was read as every organization")
	}
	if !strings.Contains(err.Error(), "BALLAST_WORKER_ORG_ID") {
		t.Fatalf("the refusal does not name the setting: %v", err)
	}
}

// What fits on the machine of the first deployment: four cores, one of them left
// to the system, so three generators; and two for one organization, so a third is
// always left for somebody else.
func TestTheDefaultSlotsFitTheFirstMachine(t *testing.T) {
	required(t)

	cfg, err := config.LoadWorker()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.MaxConcurrentRuns != 3 || cfg.MaxRunsPerOrg != 2 {
		t.Fatalf("defaults are %d and %d, want 3 and 2", cfg.MaxConcurrentRuns, cfg.MaxRunsPerOrg)
	}
}

func TestTheSlotsAreReadFromTheEnvironment(t *testing.T) {
	required(t)
	t.Setenv("BALLAST_MAX_CONCURRENT_RUNS", "6")
	t.Setenv("BALLAST_MAX_RUNS_PER_ORG", "4")

	cfg, err := config.LoadWorker()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.MaxConcurrentRuns != 6 || cfg.MaxRunsPerOrg != 4 {
		t.Fatalf("slots = %d and %d, want 6 and 4", cfg.MaxConcurrentRuns, cfg.MaxRunsPerOrg)
	}
}

// A value that does not say how many is refused by name, the way every other
// setting of the worker is. Zero would be a worker that starts, says it is alive,
// and never runs anything.
func TestSlotsThatDoNotSayHowManyStopTheProcess(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"BALLAST_MAX_CONCURRENT_RUNS", "0"},
		{"BALLAST_MAX_CONCURRENT_RUNS", "-2"},
		{"BALLAST_MAX_CONCURRENT_RUNS", "many"},
		{"BALLAST_MAX_RUNS_PER_ORG", "0"},
		{"BALLAST_MAX_RUNS_PER_ORG", "-1"},
		{"BALLAST_MAX_RUNS_PER_ORG", "some"},
	} {
		// A subtest each: Setenv lasts until the end of the test, and the value
		// of one case must not be what the next one is refused for.
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			required(t)
			t.Setenv(tc.key, tc.value)

			_, err := config.LoadWorker()
			if err == nil {
				t.Fatalf("%s=%q was accepted", tc.key, tc.value)
			}
			if !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("the refusal does not name the setting: %v", err)
			}
		})
	}
}

// An organization may not be allowed more than the machine has: the share would
// be a promise the machine cannot keep, and one organization could be waiting
// for slots that never exist.
func TestAShareLargerThanTheMachineStopsTheProcess(t *testing.T) {
	required(t)
	t.Setenv("BALLAST_MAX_CONCURRENT_RUNS", "2")
	t.Setenv("BALLAST_MAX_RUNS_PER_ORG", "3")

	_, err := config.LoadWorker()
	if err == nil {
		t.Fatal("a share larger than the machine was accepted")
	}
	if !strings.Contains(err.Error(), "BALLAST_MAX_RUNS_PER_ORG") {
		t.Fatalf("the refusal does not name the setting: %v", err)
	}
}

// Every run in flight beats, watches for its cancellation and writes its lines,
// each in a short transaction. A pool no larger than the number of runs could let
// them wait on one another; it is sized from the slots instead of left at the
// driver's default.
func TestThePoolIsSizedFromTheSlots(t *testing.T) {
	required(t)
	t.Setenv("BALLAST_MAX_CONCURRENT_RUNS", "5")
	t.Setenv("BALLAST_MAX_RUNS_PER_ORG", "2")

	cfg, err := config.LoadWorker()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.PoolConnections() < 3*cfg.MaxConcurrentRuns {
		t.Fatalf("a pool of %d connections for %d runs that each use three at the busiest moment",
			cfg.PoolConnections(), cfg.MaxConcurrentRuns)
	}
}

package run

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// The ticket's acceptance criterion: the run service, decided entirely by
// itself, with a fake generator.
//
// Every one of these endings used to be covered only end to end, against a real
// Docker daemon and a real k6 — three minutes to learn that an unreadable
// summary errors the run. What the service decides is a handful of branches and
// belongs here; what a container does belongs there.

// recordingExecutor stands in for a generator. It writes down the job it was
// handed and returns what the test set up, which is all a service needs from
// one — and the proof that nothing in this package knows what a container is.
type recordingExecutor struct {
	outcome Outcome
	err     error

	mu   sync.Mutex
	jobs []Job
}

func (e *recordingExecutor) Execute(
	_ context.Context, job Job, _ chan<- domain.LogLine,
) (Outcome, error) {
	e.mu.Lock()
	e.jobs = append(e.jobs, job)
	e.mu.Unlock()

	return e.outcome, e.err
}

func (e *recordingExecutor) executed() []Job {
	e.mu.Lock()
	defer e.mu.Unlock()

	out := make([]Job, len(e.jobs))
	copy(out, e.jobs)
	return out
}

// scriptedParser stands in for the summary parser. What it extracts is tested
// in adapter/k6; here it only has to answer.
type scriptedParser struct {
	results domain.RunResults
	err     error

	read [][]byte
}

func (p *scriptedParser) Parse(raw []byte) (domain.RunResults, error) {
	p.read = append(p.read, raw)
	return p.results, p.err
}

type executionFixture struct {
	service  *Service
	runs     *fakeRuns
	executor *recordingExecutor
	parser   *scriptedParser
	orgID    uuid.UUID
	claimed  domain.Run
}

// newExecutionFixture is a worker about to execute one run: claimed, started,
// against a verified target. The test sets up what the generator answers.
func newExecutionFixture() executionFixture {
	environment := verifiedTarget()
	claimed := domain.Run{
		ID:            uuid.New(),
		EnvironmentID: environment.ID,
		ScenarioID:    uuid.New(),
		Profile:       domain.ProfileSmoke,
		ProfileParams: domain.ProfileParams{TargetVUs: 1, PlateauS: 5},
		// As the claim returns it: the run is already running by the time the
		// generator is asked for.
		Status: domain.StatusRunning,
	}

	runs := &fakeRuns{journal: &journal{}, next: &claimed}
	executor := &recordingExecutor{outcome: Outcome{Summary: []byte(`{"metrics":{}}`)}}
	parser := &scriptedParser{}

	service := NewService(
		runs,
		fakeScenarios{scenario: domain.Scenario{Script: "export default function () {}"}},
		fakeEnvironments{environment: environment},
		&fakeAllowlist{hosts: []string{"staging.example.test"}},
		&fakeSecrets{values: map[string]string{"API_TOKEN": "a-token"}},
		executor,
		&fakeContainers{journal: &journal{}},
		&fakeProjects{},
		staticOptions{},
		parser,
		allowAll{},
		fixedClock{at: time.Unix(0, 0).UTC()},
		Accepting(true),
		discardLogger(),
	)
	// The heartbeat and the cancellation watcher are not what these tests are
	// about, and a tick during one of them would only add noise.
	service.beatEvery = time.Hour
	service.watchEvery = time.Hour

	return executionFixture{
		service:  service,
		runs:     runs,
		executor: executor,
		parser:   parser,
		orgID:    uuid.New(),
		claimed:  claimed,
	}
}

// process runs the worker's one step and insists it found work.
func (f executionFixture) process(t *testing.T) {
	t.Helper()

	worked, err := f.service.ProcessNext(context.Background(), f.orgID, nil)
	if err != nil {
		t.Fatalf("ProcessNext: %v", err)
	}
	if !worked {
		t.Fatal("the worker found nothing to do")
	}
}

// Thresholds all held: the run passed, and what was measured is stored.
func TestARunWhoseThresholdsHeldPasses(t *testing.T) {
	f := newExecutionFixture()
	f.parser.results = domain.RunResults{
		Metrics: []domain.RunMetric{
			{Metric: "http_req_duration", Stat: "p(95)", Value: 42},
		},
		Thresholds: []domain.ThresholdResult{
			{Metric: "http_req_failed", Expression: "rate<0.1", Passed: true},
		},
	}

	f.process(t)

	if f.runs.finished != domain.StatusPassed {
		t.Fatalf("the run ended as %q, wanted passed", f.runs.finished)
	}
	if f.runs.savedFor != f.claimed.ID || len(f.runs.saved.Metrics) != 1 {
		t.Fatalf("what was stored for %s is %+v", f.runs.savedFor, f.runs.saved)
	}
	// The generator's own report, handed over untouched: filtering it is the
	// parser's job, and this service never looks inside.
	if len(f.parser.read) != 1 || string(f.parser.read[0]) != `{"metrics":{}}` {
		t.Fatalf("the parser was handed %q", f.parser.read)
	}
}

// One verdict that did not hold fails the run. The verdict itself is k6's,
// never recomputed here; what this decides is only how verdicts add up.
func TestARunWithAThresholdThatDidNotHoldFails(t *testing.T) {
	f := newExecutionFixture()
	f.parser.results = domain.RunResults{
		Thresholds: []domain.ThresholdResult{
			{Metric: "http_req_failed", Expression: "rate<0.1", Passed: true},
			{Metric: "http_req_duration", Expression: "p(95)<200", Passed: false},
		},
	}

	f.process(t)

	if f.runs.finished != domain.StatusFailed {
		t.Fatalf("the run ended as %q, wanted failed", f.runs.finished)
	}
	// Stored even so: a failed run is a measurement, and its numbers are the
	// point of looking at it.
	if len(f.runs.saved.Thresholds) != 2 {
		t.Fatalf("the verdicts were not stored: %+v", f.runs.saved)
	}
}

// Killed at the environment's ceiling. Whatever it managed to report is not a
// measurement of anything, so nothing is stored and nothing is parsed.
func TestAGeneratorStoppedAtItsCeilingIsATimeout(t *testing.T) {
	f := newExecutionFixture()
	f.executor.outcome = Outcome{Summary: []byte(`{"metrics":{}}`), TimedOut: true}

	f.process(t)

	if f.runs.finished != domain.StatusTimeout {
		t.Fatalf("the run ended as %q, wanted timeout", f.runs.finished)
	}
	if len(f.parser.read) != 0 {
		t.Error("a report from a run that was killed at its ceiling was read")
	}
	if f.runs.savedFor != uuid.Nil {
		t.Error("results were stored for a run that never finished")
	}
	if !strings.Contains(f.runs.finishedMessage, "maximum duration") {
		t.Errorf("the run says %q", f.runs.finishedMessage)
	}
}

// The generator could not be run at all — no image, no daemon, a job the runner
// refuses. The run errors rather than staying running for ever.
func TestAGeneratorThatCouldNotRunErrorsTheRun(t *testing.T) {
	f := newExecutionFixture()
	f.executor.err = errors.New("the egress proxy image is not on this machine")

	f.process(t)

	if f.runs.finished != domain.StatusErrored {
		t.Fatalf("the run ended as %q, wanted errored", f.runs.finished)
	}
	if !strings.Contains(f.runs.finishedMessage, "egress proxy image") {
		t.Errorf("the run says %q, which does not say what went wrong", f.runs.finishedMessage)
	}
}

// A report that cannot be read. The run errors, and says so in words somebody
// reading their history can act on.
func TestASummaryThatCannotBeReadErrorsTheRun(t *testing.T) {
	f := newExecutionFixture()
	f.parser.err = errors.New("unexpected end of JSON input")

	f.process(t)

	if f.runs.finished != domain.StatusErrored {
		t.Fatalf("the run ended as %q, wanted errored", f.runs.finished)
	}
	if !strings.Contains(f.runs.finishedMessage, "could not be read") {
		t.Errorf("the run says %q", f.runs.finishedMessage)
	}
}

// A generator that failed for its own reasons rather than on a threshold: a bad
// exit code with no verdict to explain it.
func TestAGeneratorThatExitedBadlyWithNoVerdictErrorsTheRun(t *testing.T) {
	f := newExecutionFixture()
	f.executor.outcome = Outcome{Summary: []byte(`{"metrics":{}}`), ExitCode: 107}

	f.process(t)

	if f.runs.finished != domain.StatusErrored {
		t.Fatalf("the run ended as %q, wanted errored", f.runs.finished)
	}
	if !strings.Contains(f.runs.finishedMessage, "107") {
		t.Errorf("the run says %q, without the code", f.runs.finishedMessage)
	}
}

// The subtle one. k6 reports a threshold failure with a non-zero exit code too,
// so a bad code with verdicts to explain it is a failed run and not an errored
// one — the test ran, it simply did not pass.
func TestAGeneratorThatExitedBadlyOnAThresholdOnlyFailsTheRun(t *testing.T) {
	f := newExecutionFixture()
	f.executor.outcome = Outcome{Summary: []byte(`{"metrics":{}}`), ExitCode: 99}
	f.parser.results = domain.RunResults{
		Thresholds: []domain.ThresholdResult{
			{Metric: "http_req_duration", Expression: "p(95)<200", Passed: false},
		},
	}

	f.process(t)

	if f.runs.finished != domain.StatusFailed {
		t.Fatalf("the run ended as %q, wanted failed", f.runs.finished)
	}
}

// A cancelled execution is a cancelled run, not an errored one. This is the
// branch the cancellation and the collector both arrive through.
func TestACancelledExecutionEndsTheRunAsCancelled(t *testing.T) {
	f := newExecutionFixture()
	f.executor.err = context.Canceled

	f.process(t)

	if f.runs.finished != domain.StatusCancelled {
		t.Fatalf("the run ended as %q, wanted cancelled", f.runs.finished)
	}
}

// And the contract in the other direction: what the service builds is what the
// generator is handed. Checked here because the service is the only thing that
// knows how to build it, and nothing downstream is allowed to fill a gap.
func TestTheJobReachesTheGeneratorAsItWasBuilt(t *testing.T) {
	f := newExecutionFixture()

	f.process(t)

	executed := f.executor.executed()
	if len(executed) != 1 {
		t.Fatalf("the generator was asked %d time(s)", len(executed))
	}
	job := executed[0]

	switch {
	case job.RunID != f.claimed.ID:
		t.Errorf("the job names run %s, the claim was %s", job.RunID, f.claimed.ID)
	case string(job.Script) != "export default function () {}":
		t.Errorf("the job carries the script %q", job.Script)
	case len(job.Options) == 0:
		t.Error("the job carries no generator options")
	case job.Env["API_TOKEN"] != "a-token":
		t.Errorf("the environment's secrets did not reach the job: %v", job.Env)
	case job.Env[domain.BaseURLVariable] != "https://staging.example.test":
		t.Errorf("the job's base URL is %q", job.Env[domain.BaseURLVariable])
	case len(job.AllowedHosts) != 1 || job.AllowedHosts[0] != "staging.example.test:443":
		t.Errorf("the job may reach %v", job.AllowedHosts)
	}
}

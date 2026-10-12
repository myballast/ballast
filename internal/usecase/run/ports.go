package run

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// The interfaces below are declared here, by the code that consumes them, and
// implemented elsewhere. adapter/postgres imports this package to learn what to
// satisfy; nothing in this package ever imports an adapter.

// Repository stores runs and their results.
//
// Every method is expected to run inside a transaction scoped to orgID, which
// is what makes row-level security apply. Taking orgID as an argument rather
// than reading it from somewhere ambient keeps that visible at every call.
type Repository interface {
	Create(ctx context.Context, run domain.Run) (domain.Run, error)
	ByID(ctx context.Context, orgID, id uuid.UUID) (domain.Run, error)
	ClaimNextQueued(ctx context.Context, orgID uuid.UUID, at time.Time) (domain.Run, bool, error)
	Finish(ctx context.Context, orgID, id uuid.UUID, status domain.RunStatus, at time.Time, message string) error
	SaveResults(ctx context.Context, orgID, runID uuid.UUID, results domain.RunResults) error
	Results(ctx context.Context, orgID, runID uuid.UUID) (domain.RunResults, error)
	// InProject is a project's runs, newest first, at most limit of them.
	InProject(ctx context.Context, orgID, projectID uuid.UUID, limit int) ([]domain.Run, error)

	// The rest is what keeps a run from being abandoned without anyone
	// noticing. A worker is killed mid-run — the process, the machine, the
	// container host — and nothing above this line would ever move that run
	// out of running.

	// Heartbeat says the run is still being watched, and reports whether it is
	// still this worker's to watch. False means a collector took it, and the
	// worker that hears that stops rather than finishing a run somebody else
	// has already ended.
	Heartbeat(ctx context.Context, orgID, id uuid.UUID, at time.Time) (bool, error)
	// OrphanStale moves every run last seen before cutoff out of running, and
	// no further: a container that nobody is watching may still be running, so
	// the run is not over yet.
	OrphanStale(ctx context.Context, orgID uuid.UUID, cutoff time.Time) ([]domain.Run, error)
	// Orphaned is every run waiting to be cleaned up, including what a
	// collector that died itself left half-handled.
	Orphaned(ctx context.Context, orgID uuid.UUID) ([]domain.Run, error)
	// FailOrphaned ends an abandoned run and frees its target, and is called
	// only once what the run left behind is destroyed.
	FailOrphaned(ctx context.Context, orgID, id uuid.UUID, at time.Time, message string) error
	// LiveRunIDs is every run a container may legitimately exist for.
	LiveRunIDs(ctx context.Context, orgID uuid.UUID, since time.Time) ([]uuid.UUID, error)

	// Cancel stops a run that has not started, or asks one that has to stop,
	// and says which of the two it did. It reports domain.ErrNotCancellable
	// for a run that is already over.
	Cancel(ctx context.Context, orgID, id uuid.UUID, at time.Time, message string) (domain.CancelOutcome, error)
	// CancelEverything is the emergency stop: it cancels every queued run and
	// asks every running one to stop, reporting how many of each.
	CancelEverything(ctx context.Context, orgID uuid.UUID, at time.Time, message string) (int, int, error)
}

// LogStore keeps the lines a run's generator wrote.
//
// Both halves of the live view in one port: the worker appends while the run is
// under way, the API reads what a viewer has not seen. They are one interface
// because they are one table, and because what the reader may rely on — that a
// sequence number means the same line for ever — is a promise the writer makes.
type LogStore interface {
	// Append stores a batch. The sequence numbers are the runner's own, and a
	// line whose number is already stored is left alone: a batch written twice
	// must not become a line shown twice.
	Append(ctx context.Context, orgID, runID uuid.UUID, lines []domain.LogLine) error
	// Since returns at most limit lines after the given sequence number,
	// exclusive and in order.
	Since(ctx context.Context, orgID, runID uuid.UUID, after int64, limit int) ([]domain.LogLine, error)
}

// ProjectDirectory lists an organization's projects.
//
// Needed by the emergency stop alone, and only to ask a question about each one:
// stopping everything an organization is doing is an administrator's to do when
// they administer all of it, so the list of what "all of it" means has to come
// from somewhere. It carries no project data — this package has no business
// with any.
type ProjectDirectory interface {
	ProjectIDs(ctx context.Context, orgID uuid.UUID) ([]uuid.UUID, error)
}

// Accepting says whether this installation takes new runs at all.
//
// A named type rather than a bare bool in a long parameter list: a wiring
// mistake is then a compile error, instead of an installation that silently
// refuses every run or, worse, silently takes them during an incident.
type Accepting bool

// Containers destroys what a generator leaves on the machine it ran on.
//
// Separate from Executor because the two are asked at different moments by
// different code: Executor runs a job and waits for it, this one is handed a run
// identifier long after the process that started it is gone. A runner deployed
// on a client's own infrastructure implements both.
type Containers interface {
	// Destroy removes everything belonging to one run — containers, and the
	// network they were put on. It is expected to succeed when there is
	// nothing left to remove: two collectors cleaning up the same run are
	// doing the same work.
	Destroy(ctx context.Context, runID uuid.UUID) error
	// DestroyExcept removes everything belonging to a run that is not in keep,
	// and reports how many it removed. It is how a worker that has just
	// started gets rid of what the worker before it left behind.
	DestroyExcept(ctx context.Context, keep []uuid.UUID) (int, error)
}

// ScenarioReader reads scenarios and the thresholds they impose.
type ScenarioReader interface {
	ScenarioByID(ctx context.Context, orgID, id uuid.UUID) (domain.Scenario, error)
	ScenarioThresholds(ctx context.Context, orgID, scenarioID uuid.UUID) ([]domain.Threshold, error)
}

// EnvironmentReader reads target environments.
type EnvironmentReader interface {
	EnvironmentByID(ctx context.Context, orgID, id uuid.UUID) (domain.Environment, error)
}

// SecretReader reads an environment's secrets, decrypted, for injection into the
// generator.
//
// This is the only port in Ballast that hands a secret's value to anything, and
// the worker is the only process wired to an implementation of it: the API, which
// writes secrets, is given a port that cannot read one back. The values it
// returns go into the generator's environment and nowhere else — not into a log,
// not into a run's record, not into a response.
type SecretReader interface {
	SecretsOf(ctx context.Context, orgID, environmentID uuid.UUID) (map[string]string, error)
}

// AllowlistReader reads the hosts a project's generators may reach at all.
//
// The list is written when an administrator approves a target, and read here
// before a run starts. Two records of the same fact have to agree: an
// environment that says it is verified while its host is absent from the
// project's allowlist is an inconsistency, and a run is not the moment to paper
// over it.
type AllowlistReader interface {
	AllowedHosts(ctx context.Context, orgID, projectID uuid.UUID) ([]string, error)
}

// Job is one execution handed to a generator.
type Job struct {
	RunID uuid.UUID
	// Script is the user's scenario, untouched.
	Script []byte
	// Options is the generator's own configuration, built from the profile.
	Options []byte
	// Env is injected into the generator. It carries an environment's secrets.
	Env map[string]string
	// AllowedHosts are the host:port pairs this run may reach. Everything else
	// is refused, and in the hosted runner unreachable: the generator sits on a
	// network with no route of its own.
	AllowedHosts []string
}

// Outcome is what a finished generator left behind.
type Outcome struct {
	// Summary is the generator's raw report, unfiltered.
	Summary  []byte
	ExitCode int64
	TimedOut bool
}

// Executor runs a job to completion.
//
// This interface is the whole point of the arrangement: it is what will let a
// runner deployed on a client's own infrastructure be an addition rather than a
// rewrite. Nothing in this package knows what a container is.
type Executor interface {
	Execute(ctx context.Context, job Job, logs chan<- domain.LogLine) (Outcome, error)
}

// SummaryParser extracts the results Ballast keeps from a generator's report.
type SummaryParser interface {
	Parse(raw []byte) (domain.RunResults, error)
}

// OptionsBuilder turns a load profile into the generator's configuration.
type OptionsBuilder interface {
	Build(kind domain.ProfileKind, params domain.ProfileParams, thresholds []domain.Threshold) ([]byte, error)
}

// Authorizer answers what the caller may do. Every question it is asked names a
// project: a permission mask says what somebody may do, never what to, so a
// check without a scope is a bug rather than a refusal.
type Authorizer interface {
	Can(ctx context.Context, who domain.Identity, perm domain.Permission, projectID uuid.UUID) (bool, error)
}

// Clock exists so that what a run records about when it happened can be
// controlled in a test.
type Clock interface {
	Now() time.Time
}

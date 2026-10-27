package run

import (
	"context"
	"log/slog"
	"time"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// Service orchestrates the life of a run: accepting one, executing it, and
// recording what happened.
type Service struct {
	runs      Repository
	scenarios ScenarioReader
	// scripts, when set, is asked whether a script parses before a run is queued.
	scripts   ScriptInspector
	envs      EnvironmentReader
	allowlist AllowlistReader
	secrets   SecretReader
	executor  Executor
	cleaner   Containers
	projects  ProjectDirectory
	logs      LogStore
	journal   AuditLog
	options   OptionsBuilder
	parser    SummaryParser
	authz     Authorizer
	clock     Clock
	log       *slog.Logger

	// accepting is the platform's switch. Off, no new run is taken — at either
	// door: the API refuses one, and the worker leaves the queue alone.
	accepting bool

	// beatEvery is how often a run in progress says it is still watched.
	//
	// A field rather than the constant read where it is used, so a test can
	// watch several beats go by without taking half a minute. Nothing outside
	// this package can change it.
	beatEvery time.Duration
	// watchEvery is how often a run in progress is asked whether it has been
	// cancelled. A field for the same reason as beatEvery.
	watchEvery time.Duration
	// flushEvery is how long a batch of log lines may wait before it is
	// stored. A field for the same reason: a test should not have to sit
	// through half a second to see a line arrive.
	flushEvery time.Duration
}

// NewService wires the service. Every collaborator is an interface declared in
// this package, so nothing here depends on how any of them is implemented.
func NewService(
	runs Repository,
	scenarios ScenarioReader,
	envs EnvironmentReader,
	allowlist AllowlistReader,
	secrets SecretReader,
	executor Executor,
	cleaner Containers,
	projects ProjectDirectory,
	logs LogStore,
	journal AuditLog,
	options OptionsBuilder,
	parser SummaryParser,
	authz Authorizer,
	clock Clock,
	accepting Accepting,
	log *slog.Logger,
) *Service {
	return &Service{
		runs:      runs,
		scenarios: scenarios,
		envs:      envs,
		allowlist: allowlist,
		secrets:   secrets,
		executor:  executor,
		cleaner:   cleaner,
		projects:  projects,
		logs:      logs,
		journal:   journal,
		options:   options,
		parser:    parser,
		authz:     authz,
		clock:     clock,
		log:       log,
		accepting: bool(accepting),

		beatEvery:  domain.HeartbeatInterval,
		watchEvery: domain.CancelPollInterval,
		flushEvery: logFlushInterval,
	}
}

// CheckingScriptsWith makes the service refuse to queue a run whose script k6
// could not load. It is a method and not a parameter because most callers — the
// worker, and every test of something else — have no use for it.
func (s *Service) CheckingScriptsWith(inspector ScriptInspector) *Service {
	s.scripts = inspector
	return s
}

// record writes one line of the journal, and says so loudly when it cannot.
//
// A failure here does not undo what happened: the run was queued, or refused,
// and pretending otherwise would be a second lie on top of a missing line. What
// it must never be is silent.
func (s *Service) record(ctx context.Context, entry domain.AuditEntry) {
	if err := s.journal.Record(ctx, entry); err != nil {
		s.log.ErrorContext(ctx, "an audit entry could not be recorded",
			"action", entry.Action, "outcome", entry.Outcome,
			"org_id", entry.OrgID, "error", err)
	}
}

// refused is the entry for an attempt a guard turned away.
//
// Only the refusals a guard decided are recorded, not every rejected request: a
// profile that does not exist or parameters that describe no runnable test are
// somebody mistyping, and a journal that fills up with those is a journal
// nobody reads. What goes in is what somebody was stopped from doing.
func refused(who domain.Identity, reason string, at time.Time) domain.AuditEntry {
	return domain.Refuse(
		who.OrgID, &who.UserID, domain.ActionRunRefused, domain.TargetRun, reason, at)
}

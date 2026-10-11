package run

import (
	"log/slog"
	"time"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// Service orchestrates the life of a run: accepting one, executing it, and
// recording what happened.
type Service struct {
	runs      Repository
	scenarios ScenarioReader
	envs      EnvironmentReader
	allowlist AllowlistReader
	secrets   SecretReader
	executor  Executor
	cleaner   Containers
	projects  ProjectDirectory
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
		options:   options,
		parser:    parser,
		authz:     authz,
		clock:     clock,
		log:       log,
		accepting: bool(accepting),

		beatEvery:  domain.HeartbeatInterval,
		watchEvery: domain.CancelPollInterval,
	}
}

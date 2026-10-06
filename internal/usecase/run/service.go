package run

import (
	"log/slog"
)

// Service orchestrates the life of a run: accepting one, executing it, and
// recording what happened.
type Service struct {
	runs      Repository
	scenarios ScenarioReader
	envs      EnvironmentReader
	secrets   SecretReader
	executor  Executor
	options   OptionsBuilder
	parser    SummaryParser
	authz     Authorizer
	clock     Clock
	log       *slog.Logger
}

// NewService wires the service. Every collaborator is an interface declared in
// this package, so nothing here depends on how any of them is implemented.
func NewService(
	runs Repository,
	scenarios ScenarioReader,
	envs EnvironmentReader,
	secrets SecretReader,
	executor Executor,
	options OptionsBuilder,
	parser SummaryParser,
	authz Authorizer,
	clock Clock,
	log *slog.Logger,
) *Service {
	return &Service{
		runs:      runs,
		scenarios: scenarios,
		envs:      envs,
		secrets:   secrets,
		executor:  executor,
		options:   options,
		parser:    parser,
		authz:     authz,
		clock:     clock,
		log:       log,
	}
}

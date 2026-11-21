package apitoken

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// Service authenticates API tokens.
type Service struct {
	repo  Repository
	clock Clock
	log   *slog.Logger
}

// NewService wires the service.
func NewService(repo Repository, clock Clock, log *slog.Logger) *Service {
	return &Service{repo: repo, clock: clock, log: log}
}

// Authenticate says who is behind a secret.
//
// Every refusal is the same error, whatever the reason: telling an expired token
// from a revoked one from a forged one helps nobody but whoever is forging. The
// reason goes to the logs, with the token's id and never with the secret.
//
// A failure of the database is not a refusal. It is returned as it is, so that
// the caller can answer that Ballast is unavailable rather than that the
// credential is wrong: every pipeline would otherwise be told to rotate a good
// token in the middle of an outage.
func (s *Service) Authenticate(ctx context.Context, secret string) (domain.Identity, error) {
	// A value that cannot be a token never costs a query.
	if !domain.LooksLikeAPIToken(secret) {
		return domain.Identity{}, domain.ErrUnauthenticated
	}

	found, ok, err := s.repo.ByHash(ctx, domain.HashAPITokenSecret(secret))
	if err != nil {
		return domain.Identity{}, fmt.Errorf("look the API token up: %w", err)
	}
	if !ok {
		s.log.DebugContext(ctx, "API token refused: no such token")
		return domain.Identity{}, domain.ErrUnauthenticated
	}

	now := s.clock.Now()
	token := domain.APIToken{
		ID:        found.TokenID,
		OrgID:     found.OrgID,
		ProjectID: found.ProjectID,
		ExpiresAt: found.ExpiresAt,
		RevokedAt: found.RevokedAt,
	}
	if state := token.State(now); state != domain.APITokenActive {
		s.log.InfoContext(ctx, "API token refused",
			"reason", string(state), "token_id", found.TokenID,
			"org_id", found.OrgID, "project_id", found.ProjectID)
		return domain.Identity{}, domain.ErrUnauthenticated
	}

	s.recordUse(ctx, found, now)

	return domain.Identity{OrgID: found.OrgID, TokenID: found.TokenID}, nil
}

// recordUse notes that the token was used. A failure is logged and goes no
// further: what the list says about last use is not worth a pipeline that stops.
func (s *Service) recordUse(ctx context.Context, found Found, at time.Time) {
	if err := s.repo.Touch(ctx, found.OrgID, found.TokenID, at); err != nil {
		s.log.WarnContext(ctx, "could not record the use of an API token",
			"token_id", found.TokenID, "org_id", found.OrgID, "error", err)
	}
}

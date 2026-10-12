package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// ErrUnknownProvider reports a sign-in through a provider that is not
// configured.
var ErrUnknownProvider = errors.New("unknown identity provider")

// What a first sign-in creates, so that the new user has somewhere to work.
// The role is held on a project because that is where membership lives.
const (
	firstProjectName = "My first project"
	firstProjectSlug = "my-first-project"
)

// Service signs people in.
type Service struct {
	accounts  Accounts
	providers map[string]IdentityProvider
	clock     Clock
	log       *slog.Logger
}

// NewService wires the service. providers is keyed by the name that appears in
// the sign-in URL; a provider left out is simply not offered.
func NewService(
	accounts Accounts,
	providers map[string]IdentityProvider,
	clock Clock,
	log *slog.Logger,
) *Service {
	return &Service{accounts: accounts, providers: providers, clock: clock, log: log}
}

// Providers lists the configured providers by name.
func (s *Service) Providers() []string {
	names := make([]string, 0, len(s.providers))
	for name := range s.providers {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// LoginURL is where to send a browser that wants to sign in with provider.
func (s *Service) LoginURL(provider, state, verifier string) (string, error) {
	p, ok := s.providers[provider]
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrUnknownProvider, provider)
	}
	return p.AuthCodeURL(state, verifier), nil
}

// Complete finishes a sign-in: it trades the provider's code for an identity,
// then signs that identity in.
func (s *Service) Complete(ctx context.Context, provider, code, verifier string) (domain.Identity, error) {
	p, ok := s.providers[provider]
	if !ok {
		return domain.Identity{}, fmt.Errorf("%w: %q", ErrUnknownProvider, provider)
	}

	external, err := p.Exchange(ctx, code, verifier)
	if err != nil {
		return domain.Identity{}, fmt.Errorf("exchange the code with %s: %w", provider, err)
	}

	return s.SignIn(ctx, external)
}

// SignIn returns the identity an external one maps to, creating the user — and
// an organization for them to administer — on their first sign-in.
func (s *Service) SignIn(ctx context.Context, external domain.ExternalIdentity) (domain.Identity, error) {
	if err := external.Validate(); err != nil {
		return domain.Identity{}, err
	}

	found, err := s.accounts.Lookup(ctx, external.Subject)
	if err != nil {
		return domain.Identity{}, fmt.Errorf("look the user up: %w", err)
	}
	if len(found) > 0 {
		// Oldest first. Until invitations exist there is only ever one.
		return found[0], nil
	}

	account := NewAccount{
		OrgID:       uuid.New(),
		OrgName:     organizationName(external),
		UserID:      uuid.New(),
		Subject:     external.Subject,
		Email:       external.Email,
		Name:        external.Name,
		ProjectID:   uuid.New(),
		ProjectName: firstProjectName,
		ProjectSlug: firstProjectSlug,
		Role:        domain.RoleAdmin,
		CreatedAt:   s.clock.Now(),
	}
	if err := s.accounts.Create(ctx, account); err != nil {
		return domain.Identity{}, fmt.Errorf("create the account: %w", err)
	}

	s.log.InfoContext(ctx, "account created", "org_id", account.OrgID, "user_id", account.UserID)

	return domain.Identity{UserID: account.UserID, OrgID: account.OrgID}, nil
}

// organizationName names a new organization after the person who created it.
// They can rename it; it only has to be recognisable until then.
func organizationName(external domain.ExternalIdentity) string {
	owner := strings.TrimSpace(external.Name)
	if owner == "" {
		owner, _, _ = strings.Cut(external.Email, "@")
	}
	return owner + "'s organization"
}

// Me returns who is signed in. A session whose user no longer exists is not a
// session: it is reported as unauthenticated, not as a missing resource.
func (s *Service) Me(ctx context.Context, who domain.Identity) (Me, error) {
	me, err := s.accounts.Me(ctx, who)
	if errors.Is(err, domain.ErrNotFound) {
		return Me{}, fmt.Errorf("%w: the session's user no longer exists", domain.ErrUnauthenticated)
	}
	if err != nil {
		return Me{}, fmt.Errorf("read the signed-in user: %w", err)
	}
	return me, nil
}

package authz

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// ErrNoScope reports a permission check that named no project.
//
// It is a programming error, not a refusal: every permission in Ballast is held
// within a project, so a check without one is asking a question that has no
// answer. Returning false instead would read as "not allowed" and hide the bug.
var ErrNoScope = errors.New("authz: permission check without a project")

// ErrNoActor reports a check with nobody to check.
var ErrNoActor = errors.New("authz: permission check without an identity")

// Service answers what a person may do.
//
// Permissions are resolved against the database on every call rather than
// carried in the session, so revoking a role takes effect at once instead of
// when a token happens to expire.
type Service struct {
	memberships Memberships
	tokens      Tokens
	clock       Clock
	log         *slog.Logger
}

// NewService wires the service.
func NewService(memberships Memberships, log *slog.Logger) *Service {
	return &Service{memberships: memberships, log: log}
}

// WithTokens lets the service answer for API tokens too. Without it a token is
// refused everywhere: an installation that never wired tokens must not guess.
func (s *Service) WithTokens(tokens Tokens, clock Clock) *Service {
	s.tokens = tokens
	s.clock = clock
	return s
}

// Can reports whether who may do perm in projectID.
//
// who carries both the person and their organization. The two never travel
// apart: without the organization there is no tenant-scoped read, and reading
// it from somewhere ambient would hide at the call site which tenant is being
// asked about.
func (s *Service) Can(
	ctx context.Context,
	who domain.Identity,
	perm domain.Permission,
	projectID uuid.UUID,
) (bool, error) {
	if err := validate(who, projectID); err != nil {
		return false, err
	}
	if who.IsToken() {
		token, active, err := s.activeToken(ctx, who, projectID)
		if err != nil || !active {
			return false, err
		}
		return token.Allows(perm, projectID, s.clock.Now()), nil
	}

	membership, found, err := s.memberships.OfMember(ctx, who, projectID)
	if err != nil {
		return false, fmt.Errorf("read the membership: %w", err)
	}
	if !found {
		return false, nil
	}

	mask, known := membership.Permissions()
	if !known {
		// A role the code does not recognise grants nothing. It also means a
		// row holds something this build never wrote, which is worth saying out
		// loud rather than treating as an ordinary refusal.
		s.log.ErrorContext(ctx, "membership holds a role this build does not know",
			"org_id", who.OrgID, "user_id", who.UserID, "project_id", projectID,
			"role", membership.Role,
		)
		return false, nil
	}

	return mask.Has(perm), nil
}

// CanInAnyProject reports whether who holds perm in at least one project of
// their organization.
//
// It exists for the one thing that has no project yet: creating one. Every
// check it makes is still scoped to a project, so the rule stays "this person
// already administers something here" rather than an organization-wide role,
// which the permission model has no room for.
func (s *Service) CanInAnyProject(
	ctx context.Context,
	who domain.Identity,
	perm domain.Permission,
) (bool, error) {
	if err := validateActor(who); err != nil {
		return false, err
	}
	// "In any project of the organization" is a question about a person. A token
	// belongs to one project and is not an organization-wide anything.
	if who.IsToken() {
		return false, nil
	}

	memberships, err := s.memberships.AllOfMember(ctx, who)
	if err != nil {
		return false, fmt.Errorf("read the memberships: %w", err)
	}

	for _, membership := range memberships {
		if mask, known := membership.Permissions(); known && mask.Has(perm) {
			return true, nil
		}
	}

	return false, nil
}

// Permissions is everything who may do in projectID, for a caller that needs
// the whole mask rather than one answer.
func (s *Service) Permissions(
	ctx context.Context,
	who domain.Identity,
	projectID uuid.UUID,
) (domain.PermissionSet, error) {
	if err := validate(who, projectID); err != nil {
		return 0, err
	}
	if who.IsToken() {
		_, active, err := s.activeToken(ctx, who, projectID)
		if err != nil || !active {
			return 0, err
		}
		return domain.APITokenPermissions(), nil
	}

	membership, found, err := s.memberships.OfMember(ctx, who, projectID)
	if err != nil {
		return 0, fmt.Errorf("read the membership: %w", err)
	}
	if !found {
		return 0, nil
	}

	mask, _ := membership.Permissions()
	return mask, nil
}

func validate(who domain.Identity, projectID uuid.UUID) error {
	if err := validateActor(who); err != nil {
		return err
	}
	if projectID == uuid.Nil {
		return ErrNoScope
	}
	return nil
}

// validateActor refuses an identity that is nobody, and one that is both a
// person and a token: nobody built that on purpose, and resolving it to
// whichever is checked first would be a guess.
func validateActor(who domain.Identity) error {
	person, token := who.UserID != uuid.Nil, who.TokenID != uuid.Nil
	if who.OrgID == uuid.Nil || person == token {
		return ErrNoActor
	}
	return nil
}

// activeToken reads the token's row and says whether it may act in projectID
// now: found, not revoked, not expired, and tied to that project. The row is
// read on every call and nothing is remembered between two.
//
// A service wired without tokens answers false, and says so in the log: that is
// an installation receiving a credential it never meant to accept.
func (s *Service) activeToken(
	ctx context.Context, who domain.Identity, projectID uuid.UUID,
) (domain.APIToken, bool, error) {
	if s.tokens == nil || s.clock == nil {
		s.log.ErrorContext(ctx, "an API token was presented to a service wired without tokens",
			"org_id", who.OrgID, "token_id", who.TokenID)
		return domain.APIToken{}, false, nil
	}

	token, found, err := s.tokens.ByID(ctx, who.OrgID, who.TokenID)
	if err != nil {
		return domain.APIToken{}, false, fmt.Errorf("read the API token: %w", err)
	}
	if !found || token.ProjectID != projectID || token.State(s.clock.Now()) != domain.APITokenActive {
		return domain.APIToken{}, false, nil
	}
	return token, true, nil
}

// MembershipOf is the caller's own membership in a project, and false when
// they hold none. It is a description, not a decision: Can is what decides.
func (s *Service) MembershipOf(
	ctx context.Context, who domain.Identity, projectID uuid.UUID,
) (domain.Membership, bool, error) {
	if err := validate(who, projectID); err != nil {
		return domain.Membership{}, false, err
	}
	// A token is not a member of anything.
	if who.IsToken() {
		return domain.Membership{}, false, nil
	}
	return s.memberships.OfMember(ctx, who, projectID)
}

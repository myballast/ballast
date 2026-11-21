package authz

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// A token is not a person, and Can is where the difference is decided. The rules
// these tests hold in place:
//
//   - the answer comes from the token's row, read on every call, so revoking a
//     token takes effect on the next request rather than when something expires;
//   - a token holds two permissions in one project and nothing else, whatever the
//     role of whoever created it;
//   - anything unclear fails closed: a service built without tokens, an identity
//     that is both a person and a token, a row that cannot be found.
//
// Written before the code, which is why they do not compile yet.

type fakeClock struct{ at time.Time }

func (c fakeClock) Now() time.Time { return c.at }

type fakeTokens struct {
	byID  map[uuid.UUID]domain.APIToken
	err   error
	reads int
}

func (f *fakeTokens) ByID(_ context.Context, org, id uuid.UUID) (domain.APIToken, bool, error) {
	f.reads++
	if f.err != nil {
		return domain.APIToken{}, false, f.err
	}
	token, ok := f.byID[id]
	if ok && token.OrgID != org {
		// The repository reads under the organization: another one's token is
		// not there.
		return domain.APIToken{}, false, nil
	}
	return token, ok, nil
}

var tokenNow = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

func aToken(project uuid.UUID) (domain.APIToken, domain.Identity) {
	token := domain.APIToken{
		ID:        uuid.New(),
		OrgID:     uuid.New(),
		ProjectID: project,
		Name:      "ci",
		ExpiresAt: tokenNow.Add(24 * time.Hour),
	}
	return token, domain.Identity{OrgID: token.OrgID, TokenID: token.ID}
}

func serviceWithTokens(tokens *fakeTokens) *Service {
	return newService(fakeMemberships{}).WithTokens(tokens, fakeClock{at: tokenNow})
}

func TestATokenMayViewAndRunInItsProject(t *testing.T) {
	project := uuid.New()
	token, who := aToken(project)
	svc := serviceWithTokens(&fakeTokens{byID: map[uuid.UUID]domain.APIToken{token.ID: token}})

	for _, perm := range []domain.Permission{domain.PermViewProject, domain.PermRunTest} {
		allowed, err := svc.Can(context.Background(), who, perm, project)
		if err != nil || !allowed {
			t.Errorf("permission %d: allowed=%v err=%v, want allowed", perm, allowed, err)
		}
	}
}

// Whatever role created the token, it holds no more than the two. The creator
// being an administrator is exactly why: a token must not turn out to be an
// administrator's session in a CI variable.
func TestATokenMayDoNothingElse(t *testing.T) {
	project := uuid.New()
	token, who := aToken(project)
	svc := serviceWithTokens(&fakeTokens{byID: map[uuid.UUID]domain.APIToken{token.ID: token}})

	for _, perm := range []domain.Permission{
		domain.PermCancelRun,
		domain.PermManageScenarios,
		domain.PermManageEnvironments,
		domain.PermApproveTarget,
		domain.PermManageMembers,
		domain.PermManageProject,
	} {
		allowed, err := svc.Can(context.Background(), who, perm, project)
		if err != nil || allowed {
			t.Errorf("permission %d: allowed=%v err=%v, want refused", perm, allowed, err)
		}
	}
}

func TestATokenMayDoNothingInAnotherProject(t *testing.T) {
	token, who := aToken(uuid.New())
	svc := serviceWithTokens(&fakeTokens{byID: map[uuid.UUID]domain.APIToken{token.ID: token}})

	for _, perm := range []domain.Permission{domain.PermViewProject, domain.PermRunTest} {
		allowed, err := svc.Can(context.Background(), who, perm, uuid.New())
		if err != nil || allowed {
			t.Errorf("permission %d in another project: allowed=%v err=%v, want refused", perm, allowed, err)
		}
	}
}

// The row is read on every call. This is the whole point of resolving
// permissions in the database and not in what the request carries: revoke the
// token, and the very next check says no.
func TestRevokingATokenTakesEffectOnTheNextCheck(t *testing.T) {
	project := uuid.New()
	token, who := aToken(project)
	tokens := &fakeTokens{byID: map[uuid.UUID]domain.APIToken{token.ID: token}}
	svc := serviceWithTokens(tokens)

	if allowed, _ := svc.Can(context.Background(), who, domain.PermRunTest, project); !allowed {
		t.Fatal("a live token was refused; the rest of the test would prove nothing")
	}

	revokedAt := tokenNow.Add(-time.Second)
	token.RevokedAt = &revokedAt
	tokens.byID[token.ID] = token

	if allowed, err := svc.Can(context.Background(), who, domain.PermRunTest, project); err != nil || allowed {
		t.Errorf("after revocation: allowed=%v err=%v, want refused", allowed, err)
	}
	if tokens.reads != 2 {
		t.Errorf("the token was read %d times for 2 checks: an answer was remembered", tokens.reads)
	}
}

func TestAnExpiredTokenIsRefused(t *testing.T) {
	project := uuid.New()
	token, who := aToken(project)
	token.ExpiresAt = tokenNow
	svc := serviceWithTokens(&fakeTokens{byID: map[uuid.UUID]domain.APIToken{token.ID: token}})

	if allowed, err := svc.Can(context.Background(), who, domain.PermRunTest, project); err != nil || allowed {
		t.Errorf("a token at its expiry: allowed=%v err=%v, want refused", allowed, err)
	}
}

func TestATokenThatCannotBeFoundIsRefused(t *testing.T) {
	project := uuid.New()
	_, who := aToken(project)
	svc := serviceWithTokens(&fakeTokens{byID: map[uuid.UUID]domain.APIToken{}})

	if allowed, err := svc.Can(context.Background(), who, domain.PermRunTest, project); err != nil || allowed {
		t.Errorf("a deleted token: allowed=%v err=%v, want refused", allowed, err)
	}
}

// A token read under another organization is not found: the organization in the
// identity is the one the lookup used, and nothing here may widen it.
func TestATokenOfAnotherOrganizationIsRefused(t *testing.T) {
	project := uuid.New()
	token, who := aToken(project)
	who.OrgID = uuid.New()
	svc := serviceWithTokens(&fakeTokens{byID: map[uuid.UUID]domain.APIToken{token.ID: token}})

	if allowed, err := svc.Can(context.Background(), who, domain.PermRunTest, project); err != nil || allowed {
		t.Errorf("a token under another organization: allowed=%v err=%v, want refused", allowed, err)
	}
}

func TestAFailureToReadATokenIsAnErrorNotARefusal(t *testing.T) {
	outage := errors.New("connection refused")
	project := uuid.New()
	_, who := aToken(project)
	svc := serviceWithTokens(&fakeTokens{err: outage})

	allowed, err := svc.Can(context.Background(), who, domain.PermRunTest, project)
	if allowed {
		t.Error("a failed read allowed the request")
	}
	if !errors.Is(err, outage) {
		t.Errorf("error %v, want the cause kept", err)
	}
}

// A service wired without tokens must not guess. Anything but a refusal here
// would be a token accepted by an installation that never meant to have them.
func TestWithoutATokenSourceATokenIsRefused(t *testing.T) {
	project := uuid.New()
	_, who := aToken(project)
	svc := newService(fakeMemberships{})

	if allowed, err := svc.Can(context.Background(), who, domain.PermRunTest, project); err != nil || allowed {
		t.Errorf("no token source: allowed=%v err=%v, want refused", allowed, err)
	}
}

// Both a person and a token is not an identity anybody built on purpose. It is
// refused outright, not resolved to whichever happens to be checked first.
func TestAnIdentityThatIsBothAPersonAndATokenIsNobody(t *testing.T) {
	project := uuid.New()
	token, who := aToken(project)
	who.UserID = uuid.New()
	svc := serviceWithTokens(&fakeTokens{byID: map[uuid.UUID]domain.APIToken{token.ID: token}})

	if _, err := svc.Can(context.Background(), who, domain.PermRunTest, project); !errors.Is(err, ErrNoActor) {
		t.Errorf("error %v, want ErrNoActor", err)
	}
}

func TestATokenCheckStillNeedsAProject(t *testing.T) {
	token, who := aToken(uuid.New())
	svc := serviceWithTokens(&fakeTokens{byID: map[uuid.UUID]domain.APIToken{token.ID: token}})

	if _, err := svc.Can(context.Background(), who, domain.PermRunTest, uuid.Nil); !errors.Is(err, ErrNoScope) {
		t.Errorf("error %v, want ErrNoScope: a check without a project is a bug, for a token too", err)
	}
}

// Routes that ask "in any project of the organization" are for people. A token
// belongs to one project and is not an organization-wide anything.
func TestATokenIsNeverAllowedInAnyProject(t *testing.T) {
	project := uuid.New()
	token, who := aToken(project)
	svc := serviceWithTokens(&fakeTokens{byID: map[uuid.UUID]domain.APIToken{token.ID: token}})

	for _, perm := range []domain.Permission{domain.PermViewProject, domain.PermRunTest, domain.PermManageProject} {
		allowed, err := svc.CanInAnyProject(context.Background(), who, perm)
		if err != nil || allowed {
			t.Errorf("permission %d in any project: allowed=%v err=%v, want refused", perm, allowed, err)
		}
	}
}

func TestATokenHasNoMembershipAndTheMaskIsTheTokens(t *testing.T) {
	project := uuid.New()
	token, who := aToken(project)
	svc := serviceWithTokens(&fakeTokens{byID: map[uuid.UUID]domain.APIToken{token.ID: token}})

	if _, found, err := svc.MembershipOf(context.Background(), who, project); err != nil || found {
		t.Errorf("a token has a membership: found=%v err=%v", found, err)
	}

	mask, err := svc.Permissions(context.Background(), who, project)
	if err != nil {
		t.Fatalf("Permissions: %v", err)
	}
	if want := domain.APITokenPermissions(); mask != want {
		t.Errorf("mask %b, want %b", mask, want)
	}

	other, err := svc.Permissions(context.Background(), who, uuid.New())
	if err != nil || other != 0 {
		t.Errorf("mask in another project %b (%v), want none", other, err)
	}
}

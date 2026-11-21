package apitoken_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/apitoken"
)

// Managing tokens is what a project administrator does from the screen: create,
// list and revoke. What these tests hold in place:
//
//   - the permission is checked here and not only by the route, before anything
//     is read — somebody who may not manage the project learns nothing about it;
//   - a token can never manage tokens, whatever the authorizer says: one that
//     could would outlive its own revocation;
//   - the secret exists in the result of Create and nowhere else, not in the
//     journal, not in a log, not in what is stored;
//   - what is written and what is recorded travel together, or not at all.
//
// Written before the code, which is why they do not compile yet.

type fakeAuthorizer struct {
	allowed bool
	err     error
	asked   []domain.Permission
	project []uuid.UUID
}

func (f *fakeAuthorizer) Can(_ context.Context, _ domain.Identity, perm domain.Permission, project uuid.UUID) (bool, error) {
	f.asked = append(f.asked, perm)
	f.project = append(f.project, project)
	return f.allowed, f.err
}

type created struct {
	token domain.APIToken
	hash  string
	entry domain.AuditEntry
}

type revoked struct {
	token domain.APIToken
	at    time.Time
	entry domain.AuditEntry
}

type fakeRegistry struct {
	byID      map[uuid.UUID]domain.APIToken
	listed    []apitoken.Listed
	listedFor []uuid.UUID

	creates []created
	revokes []revoked
	reads   int

	createErr error
	revokeErr error
	readErr   error
}

func (f *fakeRegistry) ByID(_ context.Context, org, id uuid.UUID) (domain.APIToken, bool, error) {
	f.reads++
	if f.readErr != nil {
		return domain.APIToken{}, false, f.readErr
	}
	token, ok := f.byID[id]
	if ok && token.OrgID != org {
		return domain.APIToken{}, false, nil
	}
	return token, ok, nil
}

func (f *fakeRegistry) Create(_ context.Context, token domain.APIToken, hash string, entry domain.AuditEntry) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.creates = append(f.creates, created{token, hash, entry})
	return nil
}

func (f *fakeRegistry) List(_ context.Context, org, project uuid.UUID) ([]apitoken.Listed, error) {
	f.listedFor = append(f.listedFor, org, project)
	return f.listed, nil
}

func (f *fakeRegistry) Revoke(_ context.Context, token domain.APIToken, at time.Time, entry domain.AuditEntry) error {
	if f.revokeErr != nil {
		return f.revokeErr
	}
	f.revokes = append(f.revokes, revoked{token, at, entry})
	return nil
}

type managerSetup struct {
	manager  *apitoken.Manager
	registry *fakeRegistry
	authz    *fakeAuthorizer
	logs     *bytes.Buffer
	who      domain.Identity
	project  uuid.UUID
}

func newManager(t *testing.T) managerSetup {
	t.Helper()

	registry := &fakeRegistry{byID: map[uuid.UUID]domain.APIToken{}}
	authz := &fakeAuthorizer{allowed: true}
	logs := &bytes.Buffer{}
	random := bytes.NewReader(bytes.Repeat([]byte{7}, 64))

	return managerSetup{
		manager: apitoken.NewManager(registry, authz, fakeClock{at: now}, random,
			slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))),
		registry: registry,
		authz:    authz,
		logs:     logs,
		who:      domain.Identity{UserID: uuid.New(), OrgID: uuid.New()},
		project:  uuid.New(),
	}
}

// stored puts a token of the project in the registry.
func (s managerSetup) stored(mutate func(*domain.APIToken)) domain.APIToken {
	token := domain.APIToken{
		ID:        uuid.New(),
		OrgID:     s.who.OrgID,
		ProjectID: s.project,
		Name:      "ci",
		Prefix:    "blt_Ab3dEf9h",
		ExpiresAt: now.Add(24 * time.Hour),
		CreatedAt: now.Add(-time.Hour),
	}
	if mutate != nil {
		mutate(&token)
	}
	s.registry.byID[token.ID] = token
	return token
}

// --- Create -------------------------------------------------------------------

func TestAnAdministratorCreatesAToken(t *testing.T) {
	s := newManager(t)

	got, err := s.manager.Create(context.Background(), s.who, s.project, apitoken.CreateCommand{Name: "  github main  "})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if len(s.registry.creates) != 1 {
		t.Fatalf("%d tokens written, want 1", len(s.registry.creates))
	}
	w := s.registry.creates[0]

	if !domain.LooksLikeAPIToken(got.Secret) {
		t.Errorf("the secret %q does not look like one", got.Secret)
	}
	// What is stored is the hash of what was returned, so the secret that is
	// shown is the one that works.
	if w.hash != domain.HashAPITokenSecret(got.Secret) {
		t.Error("the stored hash is not the hash of the secret that was returned")
	}

	if w.token.OrgID != s.who.OrgID || w.token.ProjectID != s.project {
		t.Errorf("written under org %s project %s, want %s %s", w.token.OrgID, w.token.ProjectID, s.who.OrgID, s.project)
	}
	if w.token.Name != "github main" {
		t.Errorf("name %q, want it trimmed", w.token.Name)
	}
	if w.token.Prefix != domain.APITokenDisplayPrefix(got.Secret) {
		t.Errorf("prefix %q, want the display prefix of the secret", w.token.Prefix)
	}
	if w.token.CreatedBy == nil || *w.token.CreatedBy != s.who.UserID {
		t.Errorf("created by %v, want %s", w.token.CreatedBy, s.who.UserID)
	}
	if !w.token.CreatedAt.Equal(now) {
		t.Errorf("created at %v, want %v", w.token.CreatedAt, now)
	}
	if w.token.RevokedAt != nil || w.token.LastUsedAt != nil {
		t.Error("a new token says it was revoked or used")
	}
	if want := now.Add(domain.APITokenDefaultTTL); !w.token.ExpiresAt.Equal(want) {
		t.Errorf("expires at %v, want the default %v", w.token.ExpiresAt, want)
	}

	// What comes back describes the same token.
	if got.Token.ID != w.token.ID || got.Token.Prefix != w.token.Prefix {
		t.Errorf("the result describes %+v, not the token that was written", got.Token)
	}
}

func TestACreationHonoursARequestedExpiry(t *testing.T) {
	s := newManager(t)
	in30 := now.Add(30 * 24 * time.Hour)

	if _, err := s.manager.Create(context.Background(), s.who, s.project, apitoken.CreateCommand{Name: "ci", ExpiresAt: &in30}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got := s.registry.creates[0].token.ExpiresAt; !got.Equal(in30) {
		t.Errorf("expires at %v, want %v", got, in30)
	}
}

// What was written and what was journaled are one fact. The entry is handed to
// the registry together with the token, and the registry writes both in one
// transaction: if the entry cannot be written, the token does not exist.
func TestTheEntryTravelsWithTheToken(t *testing.T) {
	s := newManager(t)

	if _, err := s.manager.Create(context.Background(), s.who, s.project, apitoken.CreateCommand{Name: "ci"}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	w := s.registry.creates[0]
	want := domain.APITokenCreatedEntry(&s.who.UserID, w.token, now)

	if w.entry.Action != want.Action || w.entry.Outcome != want.Outcome || w.entry.TargetType != want.TargetType {
		t.Errorf("entry is %q / %q / %q", w.entry.Action, w.entry.Outcome, w.entry.TargetType)
	}
	if w.entry.TargetID == nil || *w.entry.TargetID != w.token.ID || w.entry.ProjectID == nil || *w.entry.ProjectID != s.project {
		t.Errorf("the entry points at %v in %v", w.entry.TargetID, w.entry.ProjectID)
	}
	if w.entry.ActorID == nil || *w.entry.ActorID != s.who.UserID {
		t.Errorf("the entry names %v, want %s", w.entry.ActorID, s.who.UserID)
	}
	if len(w.entry.Details) != 2 || w.entry.Details["prefix"] != w.token.Prefix {
		t.Errorf("details %v, want the closed set of the creation entry", w.entry.Details)
	}
}

// The secret exists in the result of Create and nowhere else.
func TestTheSecretIsNeverStoredJournaledOrLogged(t *testing.T) {
	s := newManager(t)

	got, err := s.manager.Create(context.Background(), s.who, s.project, apitoken.CreateCommand{Name: "ci"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	w := s.registry.creates[0]
	dump := w.token.Name + w.token.Prefix + w.hash
	for k, v := range w.entry.Details {
		dump += k + v
	}
	if strings.Contains(dump, got.Secret) {
		t.Error("the secret is in what was stored or journaled")
	}
	if strings.Contains(s.logs.String(), got.Secret) || strings.Contains(s.logs.String(), got.Secret[:len(got.Secret)/2]) {
		t.Errorf("the secret, or half of it, is in the logs: %s", s.logs.String())
	}
	// The log may say that a token was created and by whom; it does say so.
	if !strings.Contains(s.logs.String(), w.token.ID.String()) {
		t.Errorf("the log does not name the token: %s", s.logs.String())
	}
}

func TestAMistakeInTheRequestWritesNothing(t *testing.T) {
	cases := map[string]apitoken.CreateCommand{
		"no name":         {Name: "   "},
		"a name too long": {Name: strings.Repeat("a", 65)},
		"a control name":  {Name: "two\nlines"},
	}
	past := now.Add(-time.Hour)
	cases["an expiry in the past"] = apitoken.CreateCommand{Name: "ci", ExpiresAt: &past}
	far := now.Add(2 * domain.APITokenMaxTTL)
	cases["an expiry beyond the ceiling"] = apitoken.CreateCommand{Name: "ci", ExpiresAt: &far}

	for name, cmd := range cases {
		s := newManager(t)

		_, err := s.manager.Create(context.Background(), s.who, s.project, cmd)
		if !errors.Is(err, domain.ErrInvalidAPIToken) {
			t.Errorf("%s: error %v, want ErrInvalidAPIToken", name, err)
		}
		if len(s.registry.creates) != 0 {
			t.Errorf("%s: a token was written for a request that was refused", name)
		}
	}
}

// Without randomness there is no token. A predictable credential proves nothing
// about who holds it, and a shorter one is worse than none.
func TestACreationFailsWhenRandomnessFails(t *testing.T) {
	registry := &fakeRegistry{byID: map[uuid.UUID]domain.APIToken{}}
	authz := &fakeAuthorizer{allowed: true}
	m := apitoken.NewManager(registry, authz, fakeClock{at: now}, bytes.NewReader([]byte{1, 2, 3}),
		slog.New(slog.NewTextHandler(io.Discard, nil)))

	got, err := m.Create(context.Background(), domain.Identity{UserID: uuid.New(), OrgID: uuid.New()}, uuid.New(), apitoken.CreateCommand{Name: "ci"})
	if err == nil || got.Secret != "" {
		t.Errorf("a token was made from 3 random bytes: %q, %v", got.Secret, err)
	}
	if len(registry.creates) != 0 {
		t.Error("a token was written without a secret")
	}
}

// The ceiling is the registry's to enforce, in the transaction that writes, so
// that two creations at once cannot both pass. What the manager owes is that the
// refusal comes back as itself and that no secret goes with it.
func TestTheCeilingComesBackAsItselfWithNoSecret(t *testing.T) {
	s := newManager(t)
	s.registry.createErr = domain.ErrTooManyAPITokens

	got, err := s.manager.Create(context.Background(), s.who, s.project, apitoken.CreateCommand{Name: "ci"})

	if !errors.Is(err, domain.ErrTooManyAPITokens) {
		t.Errorf("error %v, want ErrTooManyAPITokens", err)
	}
	if got.Secret != "" {
		t.Error("a secret was returned for a token that was not created")
	}
}

// --- Permission ---------------------------------------------------------------

func TestSomebodyWhoMayNotManageTheProjectCreatesNothingAndLearnsNothing(t *testing.T) {
	s := newManager(t)
	s.authz.allowed = false

	_, errCreate := s.manager.Create(context.Background(), s.who, s.project, apitoken.CreateCommand{Name: "   "})
	_, errList := s.manager.List(context.Background(), s.who, s.project)
	errRevoke := s.manager.Revoke(context.Background(), s.who, s.project, uuid.New())

	for name, err := range map[string]error{"create": errCreate, "list": errList, "revoke": errRevoke} {
		if !errors.Is(err, domain.ErrForbidden) {
			t.Errorf("%s: error %v, want ErrForbidden — and not the validation error, which would tell a stranger what is wrong with a request they may not make", name, err)
		}
	}
	if len(s.registry.creates) != 0 || len(s.registry.revokes) != 0 {
		t.Error("something was written for somebody who may not manage the project")
	}
	if s.registry.reads != 0 || len(s.registry.listedFor) != 0 {
		t.Error("the registry was read before the permission was checked")
	}
}

func TestThePermissionAskedIsManagingTheProjectInThatProject(t *testing.T) {
	s := newManager(t)

	_, _ = s.manager.Create(context.Background(), s.who, s.project, apitoken.CreateCommand{Name: "ci"})
	_, _ = s.manager.List(context.Background(), s.who, s.project)
	_ = s.manager.Revoke(context.Background(), s.who, s.project, uuid.New())

	if len(s.authz.asked) != 3 {
		t.Fatalf("the permission was asked %d times for 3 operations", len(s.authz.asked))
	}
	for i := range s.authz.asked {
		if s.authz.asked[i] != domain.PermManageProject || s.authz.project[i] != s.project {
			t.Errorf("operation %d asked %d in %s, want manage-project in %s", i, s.authz.asked[i], s.authz.project[i], s.project)
		}
	}
}

// A token that could manage tokens could mint a successor and outlive its own
// revocation. Whatever the authorizer says, a token is not an administrator.
func TestATokenCanNeverManageTokens(t *testing.T) {
	s := newManager(t)
	s.authz.allowed = true
	token := s.stored(nil)
	asToken := domain.Identity{OrgID: s.who.OrgID, TokenID: uuid.New()}

	_, errCreate := s.manager.Create(context.Background(), asToken, s.project, apitoken.CreateCommand{Name: "successor"})
	_, errList := s.manager.List(context.Background(), asToken, s.project)
	errRevoke := s.manager.Revoke(context.Background(), asToken, s.project, token.ID)

	for name, err := range map[string]error{"create": errCreate, "list": errList, "revoke": errRevoke} {
		if !errors.Is(err, domain.ErrForbidden) {
			t.Errorf("%s by a token: error %v, want ErrForbidden", name, err)
		}
	}
	if len(s.registry.creates) != 0 || len(s.registry.revokes) != 0 {
		t.Error("a token managed tokens")
	}
}

func TestAFailureToCheckThePermissionIsNotARefusal(t *testing.T) {
	s := newManager(t)
	outage := errors.New("connection refused")
	s.authz.err = outage

	_, err := s.manager.Create(context.Background(), s.who, s.project, apitoken.CreateCommand{Name: "ci"})

	if !errors.Is(err, outage) || errors.Is(err, domain.ErrForbidden) {
		t.Errorf("error %v, want the cause and not a refusal", err)
	}
}

// --- List ---------------------------------------------------------------------

func TestTheListSaysWhatBecameOfEachToken(t *testing.T) {
	s := newManager(t)
	revokedAt := now.Add(-time.Hour)

	active := domain.APIToken{ID: uuid.New(), Name: "active", ExpiresAt: now.Add(time.Hour), CreatedAt: now.Add(-3 * time.Hour)}
	expired := domain.APIToken{ID: uuid.New(), Name: "expired", ExpiresAt: now.Add(-time.Hour), CreatedAt: now.Add(-4 * time.Hour)}
	withdrawn := domain.APIToken{ID: uuid.New(), Name: "revoked", ExpiresAt: now.Add(time.Hour), RevokedAt: &revokedAt, CreatedAt: now.Add(-2 * time.Hour)}
	s.registry.listed = []apitoken.Listed{
		{APIToken: active, CreatedByName: "Ada"},
		{APIToken: expired},
		{APIToken: withdrawn, CreatedByName: "Grace"},
	}

	items, err := s.manager.List(context.Background(), s.who, s.project)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	states := map[string]domain.APITokenState{}
	names := map[string]string{}
	for _, item := range items {
		states[item.Token.Name] = item.State
		names[item.Token.Name] = item.CreatedByName
	}
	if states["active"] != domain.APITokenActive || states["expired"] != domain.APITokenExpired || states["revoked"] != domain.APITokenRevoked {
		t.Errorf("states %v", states)
	}
	if names["active"] != "Ada" || names["revoked"] != "Grace" {
		t.Errorf("who created them: %v", names)
	}
	// The registry is asked under the caller's organization and for the project
	// named, and for nothing else.
	if len(s.registry.listedFor) != 2 || s.registry.listedFor[0] != s.who.OrgID || s.registry.listedFor[1] != s.project {
		t.Errorf("listed %v, want org %s and project %s", s.registry.listedFor, s.who.OrgID, s.project)
	}
}

// The ones that still work come first, newest first, and the revoked ones last:
// a list in which a withdrawn token sits above a live one is a list somebody
// reads the wrong line of.
func TestTheListPutsRevokedTokensLast(t *testing.T) {
	s := newManager(t)
	at := now.Add(-time.Minute)

	mk := func(name string, ago time.Duration, revoked bool) apitoken.Listed {
		token := domain.APIToken{ID: uuid.New(), Name: name, ExpiresAt: now.Add(time.Hour), CreatedAt: now.Add(-ago)}
		if revoked {
			token.RevokedAt = &at
		}
		return apitoken.Listed{APIToken: token}
	}
	s.registry.listed = []apitoken.Listed{
		mk("revoked-new", 1*time.Hour, true),
		mk("live-old", 5*time.Hour, false),
		mk("live-new", 2*time.Hour, false),
		mk("revoked-old", 9*time.Hour, true),
	}

	items, err := s.manager.List(context.Background(), s.who, s.project)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	var order []string
	for _, item := range items {
		order = append(order, item.Token.Name)
	}
	want := "live-new,live-old,revoked-new,revoked-old"
	if got := strings.Join(order, ","); got != want {
		t.Errorf("order %s, want %s", got, want)
	}
}

func TestAnEmptyProjectHasAnEmptyListAndNotAnError(t *testing.T) {
	s := newManager(t)

	items, err := s.manager.List(context.Background(), s.who, s.project)
	if err != nil || items == nil || len(items) != 0 {
		t.Errorf("items %v, err %v, want an empty list", items, err)
	}
}

// --- Revoke -------------------------------------------------------------------

func TestRevokingATokenRecordsWhoDidIt(t *testing.T) {
	s := newManager(t)
	token := s.stored(nil)

	if err := s.manager.Revoke(context.Background(), s.who, s.project, token.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	if len(s.registry.revokes) != 1 {
		t.Fatalf("%d revocations, want 1", len(s.registry.revokes))
	}
	r := s.registry.revokes[0]
	if r.token.ID != token.ID || !r.at.Equal(now) {
		t.Errorf("revoked %s at %v, want %s at %v", r.token.ID, r.at, token.ID, now)
	}
	if r.entry.Action != domain.ActionAPITokenRevoked || r.entry.TargetID == nil || *r.entry.TargetID != token.ID {
		t.Errorf("entry %q on %v", r.entry.Action, r.entry.TargetID)
	}
	if r.entry.ActorID == nil || *r.entry.ActorID != s.who.UserID {
		t.Errorf("the entry names %v, want %s", r.entry.ActorID, s.who.UserID)
	}
	if len(r.entry.Details) != 1 || r.entry.Details["prefix"] != token.Prefix {
		t.Errorf("details %v, want the prefix alone", r.entry.Details)
	}
}

// A token that is not in this project is not found, whichever way it is not: it
// does not exist, it belongs to another project, it belongs to another
// organization, or it was already revoked. One answer for all of them, so that
// nobody learns what exists elsewhere.
func TestATokenThatIsNotThisProjectsIsNotFound(t *testing.T) {
	s := newManager(t)
	revokedAt := now.Add(-time.Hour)

	otherProject := s.stored(func(tok *domain.APIToken) { tok.ProjectID = uuid.New() })
	otherOrg := s.stored(func(tok *domain.APIToken) { tok.OrgID = uuid.New() })
	already := s.stored(func(tok *domain.APIToken) { tok.RevokedAt = &revokedAt })

	for name, id := range map[string]uuid.UUID{
		"unknown":         uuid.New(),
		"another project": otherProject.ID,
		"another org":     otherOrg.ID,
		"already revoked": already.ID,
	} {
		err := s.manager.Revoke(context.Background(), s.who, s.project, id)
		if !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%s: error %v, want ErrNotFound", name, err)
		}
	}
	if len(s.registry.revokes) != 0 {
		t.Error("a token that is not this project's was revoked")
	}
}

// Somebody else revoked it between the read and the write: the registry says it
// matched no row, and that is a not found like any other.
func TestLosingARaceForARevocationIsNotFound(t *testing.T) {
	s := newManager(t)
	token := s.stored(nil)
	s.registry.revokeErr = domain.ErrNotFound

	if err := s.manager.Revoke(context.Background(), s.who, s.project, token.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("error %v, want ErrNotFound", err)
	}
}

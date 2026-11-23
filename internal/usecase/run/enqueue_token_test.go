package run

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// A pipeline launches a run with an API token, and meets every guardrail a person
// meets, in the same place. What these tests hold in place:
//
//   - nothing is skipped: the verified target, the project, the ceilings, the
//     window, the switch and the limits all apply to a token as they do to a person;
//   - the run and every entry about it name the token, and no person — an entry
//     with no author reads as Ballast acting alone, and a token must never read as
//     that;
//   - the hourly limit counts a token's own runs, and the organization's limits
//     still count it.
//
// Written before the code, which is why they do not compile yet.

// asToken is the fixture with a token where the person was.
func asToken(f fixture) (fixture, domain.Identity) {
	token := domain.Identity{OrgID: uuid.New(), TokenID: uuid.New()}
	f.actor = token
	return f, token
}

func byThisToken(t *testing.T, entry domain.AuditEntry, token domain.Identity) {
	t.Helper()

	if entry.ActorTokenID == nil || *entry.ActorTokenID != token.TokenID {
		t.Errorf("the entry names the token %v, want %s", entry.ActorTokenID, token.TokenID)
	}
	if entry.ActorID != nil {
		t.Errorf("the entry names the person %v for what a token did", entry.ActorID)
	}
}

// --- What a token's run is ---------------------------------------------------------

func TestATokenQueuesARunThatRemembersTheTokenAndNoPerson(t *testing.T) {
	f, token := asToken(newFixture(t, nil))

	queued, err := f.service.Enqueue(context.Background(), f.command())
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if queued.TriggeredByToken == nil || *queued.TriggeredByToken != token.TokenID {
		t.Errorf("triggered by token %v, want %s", queued.TriggeredByToken, token.TokenID)
	}
	if queued.TriggeredBy != nil {
		t.Errorf("a run launched by a token names the person %v", queued.TriggeredBy)
	}
	// What is stored is what is returned, and it is the same run a person would
	// have queued: frozen at the scenario's version, in its project.
	if f.runs.created.TriggeredByToken == nil || *f.runs.created.TriggeredByToken != token.TokenID {
		t.Error("the run that was written does not name the token")
	}
	if queued.Status != domain.StatusQueued || queued.ScenarioVersion != f.scenario.Version || queued.ProjectID != f.scenario.ProjectID {
		t.Errorf("the run is %s at version %d in %s", queued.Status, queued.ScenarioVersion, queued.ProjectID)
	}
}

func TestTheEntryForATokensRunNamesTheTokenAndKeepsTheClosedSetOfDetails(t *testing.T) {
	f, token := asToken(newFixture(t, nil))

	queued, err := f.service.Enqueue(context.Background(), f.command())
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	entry := only(t, f.audit)
	byThisToken(t, entry, token)
	if entry.Action != domain.ActionRunQueued || entry.Outcome != domain.Allowed {
		t.Errorf("the entry says %s / %s", entry.Action, entry.Outcome)
	}
	if entry.TargetID == nil || *entry.TargetID != queued.ID {
		t.Errorf("the entry points at %v, want the run %s", entry.TargetID, queued.ID)
	}
	// The same four keys a person's run has, and nothing that says more about the
	// token: the entry's author is the token, and that is where it is read.
	want := map[string]bool{"environment_id": true, "profile": true, "target_vus": true, "duration_s": true}
	for key := range entry.Details {
		if !want[key] {
			t.Errorf("the entry carries an unexpected detail %q", key)
		}
	}
	if len(entry.Details) != len(want) {
		t.Errorf("details %v, want exactly %v", entry.Details, want)
	}
}

// The ceilings do not know who is asking.
func TestATokensRunIsClampedLikeAnyOther(t *testing.T) {
	f, _ := asToken(newFixture(t, nil))

	cmd := f.command()
	cmd.Params = &domain.ProfileParams{TargetVUs: 500, RampUpS: 30, PlateauS: 3600, RampDownS: 30}
	queued, err := f.service.Enqueue(context.Background(), cmd)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if queued.ProfileParams.TargetVUs != f.env.MaxVUs || queued.ProfileParams.TotalDuration() > f.env.MaxDurationS {
		t.Errorf("stored %d VUs for %ds, the environment allows %d for %ds",
			queued.ProfileParams.TargetVUs, queued.ProfileParams.TotalDuration(), f.env.MaxVUs, f.env.MaxDurationS)
	}
}

// --- Permission --------------------------------------------------------------------

type recordedQuestion struct {
	who       domain.Identity
	perm      domain.Permission
	projectID uuid.UUID
}

type recordingAuthorizer struct{ asked []recordedQuestion }

func (r *recordingAuthorizer) Can(
	_ context.Context, who domain.Identity, perm domain.Permission, projectID uuid.UUID,
) (bool, error) {
	r.asked = append(r.asked, recordedQuestion{who, perm, projectID})
	return true, nil
}

// The question is the one a person gets — may this caller launch a run in the
// scenario's project — and the caller it names is the token, so that the
// authorizer is what knows a token holds that permission in that project only.
func TestTheQuestionAskedAboutATokenIsTheSameAsAboutAPerson(t *testing.T) {
	f, token := asToken(newFixture(t, nil))
	asked := &recordingAuthorizer{}
	f.service.authz = asked

	if _, err := f.service.Enqueue(context.Background(), f.command()); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if len(asked.asked) != 1 {
		t.Fatalf("the authorizer was asked %d times, want once", len(asked.asked))
	}
	q := asked.asked[0]
	if q.who != token || q.perm != domain.PermRunTest || q.projectID != f.scenario.ProjectID {
		t.Errorf("asked %+v, want the token, running a test, in the scenario's project", q)
	}
}

// A token whose row says no — revoked a moment ago, or tied to another project,
// which is how a scenario of another project reaches this code — is refused, and
// the refusal is written as the token's.
func TestATokenThatMayNotRunIsRefusedAndTheRefusalNamesIt(t *testing.T) {
	f, token := asToken(newFixture(t, nil))
	f.service.authz = refuseAll{}

	_, err := f.service.Enqueue(context.Background(), f.command())

	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("got %v, want ErrForbidden", err)
	}
	if f.runs.created.ScenarioID != uuid.Nil {
		t.Error("a run was written for a token that may not run")
	}
	entry := only(t, f.audit)
	byThisToken(t, entry, token)
	if entry.Outcome != domain.Refused || entry.Action != domain.ActionRunRefused {
		t.Errorf("the entry says %s / %s", entry.Action, entry.Outcome)
	}
	if entry.ProjectID == nil || *entry.ProjectID != f.scenario.ProjectID {
		t.Errorf("the entry belongs to %v, want the scenario's project", entry.ProjectID)
	}
}

// --- The guardrails ----------------------------------------------------------------

// The control the whole product rests on, for a pipeline as for a person.
func TestATokenCannotAimAtAnUnverifiedTarget(t *testing.T) {
	for _, status := range []domain.VerificationStatus{
		domain.VerificationPending, domain.VerificationProofOK, domain.VerificationFailed,
	} {
		f, token := asToken(newFixture(t, func(_ *domain.Scenario, env *domain.Environment) {
			env.VerificationStatus = status
		}))

		_, err := f.service.Enqueue(context.Background(), f.command())

		if !errors.Is(err, domain.ErrTargetNotVerified) {
			t.Errorf("%s: got %v, want ErrTargetNotVerified", status, err)
		}
		if f.runs.created.ScenarioID != uuid.Nil {
			t.Errorf("%s: a run was written for an unverified target", status)
		}
		entry := only(t, f.audit)
		byThisToken(t, entry, token)
		if entry.Outcome != domain.Refused || entry.Details["reason"] != "the target environment has not been verified" {
			t.Errorf("%s: the entry says %s with %v", status, entry.Outcome, entry.Details)
		}
	}
}

func TestATokenCannotMixAScenarioAndATargetFromDifferentProjects(t *testing.T) {
	f, token := asToken(newFixture(t, func(_ *domain.Scenario, env *domain.Environment) {
		env.ProjectID = uuid.New()
	}))

	_, err := f.service.Enqueue(context.Background(), f.command())

	if !errors.Is(err, domain.ErrCrossProject) {
		t.Fatalf("got %v, want ErrCrossProject", err)
	}
	byThisToken(t, only(t, f.audit), token)
}

func TestATokenCannotRunOutsideTheTargetsWindow(t *testing.T) {
	f, token := asToken(newFixture(t, func(_ *domain.Scenario, env *domain.Environment) {
		env.Window = &domain.AllowedWindow{StartMinute: 20 * 60, EndMinute: 23 * 60, Zone: "UTC"}
	}))

	_, err := f.service.Enqueue(context.Background(), f.command())

	if !errors.Is(err, domain.ErrOutsideAllowedWindow) {
		t.Fatalf("got %v, want ErrOutsideAllowedWindow", err)
	}
	byThisToken(t, only(t, f.audit), token)
}

func TestATokenIsRefusedWhileTheInstallationIsNotAccepting(t *testing.T) {
	f, token := asToken(newFixture(t, nil))
	closed := NewService(
		f.runs, fakeScenarios{scenario: f.scenario}, fakeEnvironments{environment: f.env},
		&fakeAllowlist{hosts: []string{"staging.example.test"}}, noSecrets{},
		nil, nil, nil, nil, f.audit, nil, nil,
		allowAll{}, fixedClock{at: time.Unix(0, 0).UTC()}, Accepting(false), discardLogger(),
	)

	_, err := closed.Enqueue(context.Background(), f.command())

	if !errors.Is(err, domain.ErrRunsSuspended) {
		t.Fatalf("got %v, want ErrRunsSuspended", err)
	}
	// The switch is read before anything about the request, so the entry has no
	// project; it still has an author, and the author is the token.
	entry := only(t, f.audit)
	byThisToken(t, entry, token)
	if entry.ProjectID != nil {
		t.Errorf("an entry written before anything was read belongs to %v", entry.ProjectID)
	}
}

// --- The limits --------------------------------------------------------------------

func TestATokenIsRefusedOnItsOwnRate(t *testing.T) {
	f, token := asToken(newFixture(t, nil))
	f.runs.tokenTriggered = Triggered{ByToken: domain.MaxRunsPerUserPerWindow, ByOrg: 1}

	_, err := f.service.Enqueue(context.Background(), f.command())

	if !errors.Is(err, domain.ErrTooManyRuns) {
		t.Fatalf("got %v, want ErrTooManyRuns", err)
	}
	entry := only(t, f.audit)
	byThisToken(t, entry, token)
	if entry.Details["limit"] != "triggered_by_token" || entry.Details["reached"] != "30" {
		t.Errorf("the entry blames %q at %q", entry.Details["limit"], entry.Details["reached"])
	}
	if f.runs.created.ScenarioID != uuid.Nil {
		t.Error("a run was written past the token's limit")
	}
}

func TestTheLastRunAPipelineMayStillGoesThrough(t *testing.T) {
	f, _ := asToken(newFixture(t, nil))
	f.runs.tokenTriggered = Triggered{
		ByToken: domain.MaxRunsPerUserPerWindow - 1,
		ByOrg:   domain.MaxRunsPerOrgPerWindow - 1,
	}
	f.runs.queued = domain.MaxQueuedRunsPerOrg - 1

	if _, err := f.service.Enqueue(context.Background(), f.command()); err != nil {
		t.Fatalf("the last permitted run was refused: %v", err)
	}
}

// What the organization has triggered and what is waiting still count: a token
// is one way into the organization, and not a way round its limits.
func TestTheOrganizationsLimitsStillCountATokensRuns(t *testing.T) {
	for name, set := range map[string]func(*fakeRuns){
		"triggered_by_org": func(r *fakeRuns) { r.tokenTriggered = Triggered{ByOrg: domain.MaxRunsPerOrgPerWindow} },
		"queued":           func(r *fakeRuns) { r.queued = domain.MaxQueuedRunsPerOrg },
	} {
		f, token := asToken(newFixture(t, nil))
		set(f.runs)

		_, err := f.service.Enqueue(context.Background(), f.command())

		if !errors.Is(err, domain.ErrTooManyRuns) {
			t.Errorf("%s: got %v, want ErrTooManyRuns", name, err)
			continue
		}
		entry := only(t, f.audit)
		byThisToken(t, entry, token)
		if entry.Details["limit"] != name {
			t.Errorf("%s: the entry blames %q", name, entry.Details["limit"])
		}
	}
}

// A token has no person, and the person's counter is not asked about: a pipeline
// is not held to — nor spared by — what some member has done this hour.
func TestATokenIsCountedByItsOwnRunsAndNotBySomebodys(t *testing.T) {
	f, token := asToken(newFixture(t, nil))
	// A person at their limit, and nobody asked.
	f.runs.triggered = Triggered{ByUser: domain.MaxRunsPerUserPerWindow, ByOrg: 1}

	if _, err := f.service.Enqueue(context.Background(), f.command()); err != nil {
		t.Fatalf("a token was refused on a person's count: %v", err)
	}

	if !f.runs.triggeredSince.IsZero() {
		t.Error("the person's counter was read for a token")
	}
	if f.runs.tokenAskedAbout != token.TokenID {
		t.Errorf("the count was taken for %s, want the token %s", f.runs.tokenAskedAbout, token.TokenID)
	}
	if want := time.Unix(0, 0).UTC().Add(-domain.RateWindow); !f.runs.tokenSince.Equal(want) {
		t.Errorf("counted since %s, want %s", f.runs.tokenSince, want)
	}
}

// The wait a pipeline is told is when its own oldest run of the window leaves it.
func TestATokenIsToldWhenItsOwnWindowOpens(t *testing.T) {
	f, _ := asToken(newFixture(t, nil))
	now := time.Unix(0, 0).UTC()
	f.runs.tokenTriggered = Triggered{ByToken: domain.MaxRunsPerUserPerWindow}
	f.runs.tokenOldest, f.runs.tokenOldestFound = now.Add(-10*time.Minute), true

	_, err := f.service.Enqueue(context.Background(), f.command())

	var refusal TooManyRuns
	if !errors.As(err, &refusal) {
		t.Fatalf("got %v, want a TooManyRuns", err)
	}
	if want := domain.RateWindow - 10*time.Minute; refusal.RetryAfter != want {
		t.Errorf("told to wait %s, want %s", refusal.RetryAfter, want)
	}
}

// --- Reading what a token launched --------------------------------------------------

// A run launched by a pipeline is read by people, and has to say so: with the
// token's name, so that somebody looking at the list can tell which pipeline.
func TestARunLaunchedByATokenSaysWhichOne(t *testing.T) {
	f := newFixture(t, nil)
	token := uuid.New()
	run := domain.Run{ID: uuid.New(), OrgID: f.actor.OrgID, ProjectID: f.scenario.ProjectID, TriggeredByToken: &token}
	f.runs.byID = map[uuid.UUID]domain.Run{run.ID: run}
	f.runs.tokenName = "github-main"
	// A person's name that must not be taken for it.
	f.runs.userName = "Ada Lovelace"

	detail, _, err := f.service.Detail(context.Background(), f.actor, run.ID)
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}

	if detail.TriggeredByName != "API token github-main" {
		t.Errorf("triggered by %q, want the token's name, said as a token's", detail.TriggeredByName)
	}
}

func TestARunWhoseTokenHasNoNameIsStillSaidToBeATokensRun(t *testing.T) {
	f := newFixture(t, nil)
	token := uuid.New()
	run := domain.Run{ID: uuid.New(), OrgID: f.actor.OrgID, ProjectID: f.scenario.ProjectID, TriggeredByToken: &token}
	f.runs.byID = map[uuid.UUID]domain.Run{run.ID: run}

	detail, _, err := f.service.Detail(context.Background(), f.actor, run.ID)
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}

	if detail.TriggeredByName != "an API token" {
		t.Errorf("triggered by %q, want it said to be a token's all the same", detail.TriggeredByName)
	}
}

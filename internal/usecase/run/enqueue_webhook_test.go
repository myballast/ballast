package run

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// A project's webhook launches a run, and meets every guardrail a person or a token
// meets, in the same place. These tests are the token's, with a webhook where the token
// was, and what they hold in place is the same:
//
//   - nothing is skipped: the verified target, the project, the ceilings, the window,
//     the switch and the limits all apply;
//   - the run and every entry about it name the webhook, and no person;
//   - the hourly limit counts the webhook's own runs, and the organization's limits
//     still count it.
//
// Written before the code, which is why they do not compile yet.

func asWebhook(f fixture) (fixture, domain.Identity) {
	hook := domain.Identity{OrgID: uuid.New(), WebhookID: uuid.New()}
	f.actor = hook
	return f, hook
}

func byThisWebhook(t *testing.T, entry domain.AuditEntry, hook domain.Identity) {
	t.Helper()

	if entry.ActorWebhookID == nil || *entry.ActorWebhookID != hook.WebhookID {
		t.Errorf("the entry names the webhook %v, want %s", entry.ActorWebhookID, hook.WebhookID)
	}
	if entry.ActorID != nil || entry.ActorTokenID != nil {
		t.Errorf("the entry names a person %v or a token %v for what a webhook did", entry.ActorID, entry.ActorTokenID)
	}
}

func TestAWebhookQueuesARunThatRemembersItAndNoPersonOrToken(t *testing.T) {
	f, hook := asWebhook(newFixture(t, nil))

	queued, err := f.service.Enqueue(context.Background(), f.command())
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if queued.TriggeredByWebhook == nil || *queued.TriggeredByWebhook != hook.WebhookID {
		t.Errorf("triggered by webhook %v, want %s", queued.TriggeredByWebhook, hook.WebhookID)
	}
	if queued.TriggeredBy != nil || queued.TriggeredByToken != nil {
		t.Errorf("a webhook's run names the person %v or the token %v", queued.TriggeredBy, queued.TriggeredByToken)
	}
	if f.runs.created.TriggeredByWebhook == nil {
		t.Error("the run that was written does not name the webhook")
	}
	entry := only(t, f.audit)
	byThisWebhook(t, entry, hook)
	want := map[string]bool{"environment_id": true, "profile": true, "target_vus": true, "duration_s": true}
	for key := range entry.Details {
		if !want[key] {
			t.Errorf("the entry carries an unexpected detail %q", key)
		}
	}
}

func TestAWebhookThatMayNotRunIsRefusedAndTheRefusalNamesIt(t *testing.T) {
	f, hook := asWebhook(newFixture(t, nil))
	f.service.authz = refuseAll{}

	_, err := f.service.Enqueue(context.Background(), f.command())

	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("got %v, want ErrForbidden", err)
	}
	if f.runs.created.ScenarioID != uuid.Nil {
		t.Error("a run was written for a webhook that may not run")
	}
	entry := only(t, f.audit)
	byThisWebhook(t, entry, hook)
	if entry.Outcome != domain.Refused || entry.ProjectID == nil || *entry.ProjectID != f.scenario.ProjectID {
		t.Errorf("the entry says %s in %v", entry.Outcome, entry.ProjectID)
	}
}

func TestAWebhookCannotAimAtAnUnverifiedTarget(t *testing.T) {
	f, hook := asWebhook(newFixture(t, func(_ *domain.Scenario, env *domain.Environment) {
		env.VerificationStatus = domain.VerificationPending
	}))

	_, err := f.service.Enqueue(context.Background(), f.command())

	if !errors.Is(err, domain.ErrTargetNotVerified) {
		t.Fatalf("got %v, want ErrTargetNotVerified", err)
	}
	if f.runs.created.ScenarioID != uuid.Nil {
		t.Error("a run was written for an unverified target")
	}
	byThisWebhook(t, only(t, f.audit), hook)
}

func TestAWebhookCannotMixAScenarioAndATargetFromDifferentProjects(t *testing.T) {
	f, hook := asWebhook(newFixture(t, func(_ *domain.Scenario, env *domain.Environment) { env.ProjectID = uuid.New() }))

	if _, err := f.service.Enqueue(context.Background(), f.command()); !errors.Is(err, domain.ErrCrossProject) {
		t.Fatalf("got %v, want ErrCrossProject", err)
	}
	byThisWebhook(t, only(t, f.audit), hook)
}

func TestAWebhookCannotRunOutsideTheTargetsWindow(t *testing.T) {
	f, hook := asWebhook(newFixture(t, func(_ *domain.Scenario, env *domain.Environment) {
		env.Window = &domain.AllowedWindow{StartMinute: 20 * 60, EndMinute: 23 * 60, Zone: "UTC"}
	}))

	if _, err := f.service.Enqueue(context.Background(), f.command()); !errors.Is(err, domain.ErrOutsideAllowedWindow) {
		t.Fatalf("got %v, want ErrOutsideAllowedWindow", err)
	}
	byThisWebhook(t, only(t, f.audit), hook)
}

func TestAWebhooksRunIsClampedLikeAnyOther(t *testing.T) {
	f, _ := asWebhook(newFixture(t, nil))

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

// --- The limits ---------------------------------------------------------------------

func TestAWebhookIsRefusedOnItsOwnRate(t *testing.T) {
	f, hook := asWebhook(newFixture(t, nil))
	f.runs.webhookTriggered = Triggered{ByWebhook: domain.MaxRunsPerUserPerWindow, ByOrg: 1}

	_, err := f.service.Enqueue(context.Background(), f.command())

	if !errors.Is(err, domain.ErrTooManyRuns) {
		t.Fatalf("got %v, want ErrTooManyRuns", err)
	}
	entry := only(t, f.audit)
	byThisWebhook(t, entry, hook)
	if entry.Details["limit"] != "triggered_by_webhook" || entry.Details["reached"] != "30" {
		t.Errorf("the entry blames %q at %q", entry.Details["limit"], entry.Details["reached"])
	}
	if f.runs.created.ScenarioID != uuid.Nil {
		t.Error("a run was written past the webhook's limit")
	}
}

func TestTheLastRunAWebhookMayStillGoesThrough(t *testing.T) {
	f, _ := asWebhook(newFixture(t, nil))
	f.runs.webhookTriggered = Triggered{
		ByWebhook: domain.MaxRunsPerUserPerWindow - 1,
		ByOrg:     domain.MaxRunsPerOrgPerWindow - 1,
	}
	f.runs.queued = domain.MaxQueuedRunsPerOrg - 1

	if _, err := f.service.Enqueue(context.Background(), f.command()); err != nil {
		t.Fatalf("the last permitted run was refused: %v", err)
	}
}

func TestTheOrganizationsLimitsStillCountAWebhooksRuns(t *testing.T) {
	for name, set := range map[string]func(*fakeRuns){
		"triggered_by_org": func(r *fakeRuns) { r.webhookTriggered = Triggered{ByOrg: domain.MaxRunsPerOrgPerWindow} },
		"queued":           func(r *fakeRuns) { r.queued = domain.MaxQueuedRunsPerOrg },
	} {
		f, hook := asWebhook(newFixture(t, nil))
		set(f.runs)

		if _, err := f.service.Enqueue(context.Background(), f.command()); !errors.Is(err, domain.ErrTooManyRuns) {
			t.Errorf("%s: got %v, want ErrTooManyRuns", name, err)
			continue
		}
		entry := only(t, f.audit)
		byThisWebhook(t, entry, hook)
		if entry.Details["limit"] != name {
			t.Errorf("%s: the entry blames %q", name, entry.Details["limit"])
		}
	}
}

func TestAWebhookIsCountedByItsOwnRunsAndNotBySomebodysOrATokens(t *testing.T) {
	f, hook := asWebhook(newFixture(t, nil))
	f.runs.triggered = Triggered{ByUser: domain.MaxRunsPerUserPerWindow, ByOrg: 1}
	f.runs.tokenTriggered = Triggered{ByToken: domain.MaxRunsPerUserPerWindow, ByOrg: 1}

	if _, err := f.service.Enqueue(context.Background(), f.command()); err != nil {
		t.Fatalf("a webhook was refused on somebody else's count: %v", err)
	}
	if !f.runs.triggeredSince.IsZero() || f.runs.tokenAskedAbout != uuid.Nil {
		t.Error("a person's or a token's counter was read for a webhook")
	}
	if f.runs.webhookAskedAbout != hook.WebhookID {
		t.Errorf("the count was taken for %s, want the webhook %s", f.runs.webhookAskedAbout, hook.WebhookID)
	}
}

// The wait the caller is told is the one of the webhook's own window: its oldest run,
// not somebody's.
func TestAWebhookIsToldWhenItsOwnWindowOpens(t *testing.T) {
	f, _ := asWebhook(newFixture(t, nil))
	now := time.Unix(0, 0).UTC()
	f.runs.webhookTriggered = Triggered{ByWebhook: domain.MaxRunsPerUserPerWindow}
	f.runs.webhookOldest, f.runs.webhookOldestFound = now.Add(-10*time.Minute), true
	// Somebody else's oldest run is not the answer.
	f.runs.oldest, f.runs.oldestFound = now.Add(-time.Minute), true
	f.runs.tokenOldest, f.runs.tokenOldestFound = now.Add(-2*time.Minute), true

	_, err := f.service.Enqueue(context.Background(), f.command())

	var refusal TooManyRuns
	if !errors.As(err, &refusal) {
		t.Fatalf("got %v, want a TooManyRuns", err)
	}
	if want := domain.RateWindow - 10*time.Minute; refusal.RetryAfter != want {
		t.Errorf("told to wait %s, want %s", refusal.RetryAfter, want)
	}
}

// --- Reading what a webhook launched ----------------------------------------------------

func TestARunLaunchedByAWebhookSaysSo(t *testing.T) {
	f := newFixture(t, nil)
	hook := uuid.New()
	run := domain.Run{ID: uuid.New(), OrgID: f.actor.OrgID, ProjectID: f.scenario.ProjectID, TriggeredByWebhook: &hook}
	f.runs.byID = map[uuid.UUID]domain.Run{run.ID: run}
	// A person's name that must not be taken for it.
	f.runs.userName = "Ada Lovelace"

	detail, _, err := f.service.Detail(context.Background(), f.actor, run.ID)
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if detail.TriggeredByName != "the project's webhook" {
		t.Errorf("triggered by %q, want it said to be the webhook's", detail.TriggeredByName)
	}
}

package run

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// A pipeline that is stopped — a job cancelled, a deadline reached — takes down
// the load it started. What these tests hold in place:
//
//   - a token cancels the runs it launched, and only those: not a person's, not
//     another token's of the same project, whatever the project;
//   - what it is asked is "may this token run a test here", the permission it
//     has, and never the permission to cancel, which it has not and must not;
//   - every refusal is the answer a person without rights gets, and is in the
//     journal under the token's name — an entry with no author reads as Ballast
//     acting alone;
//   - a cancellation by a token is written under the token too.
//
// Written before the code, which is why they do not compile yet.

// recordingTokens is the authorizer of tokensOf, remembering what it was asked.
type recordingTokens struct {
	tokensOf
	asked []domain.Permission
}

func (a *recordingTokens) Can(
	ctx context.Context, who domain.Identity, perm domain.Permission, projectID uuid.UUID,
) (bool, error) {
	a.asked = append(a.asked, perm)
	// The real one grants a token only what it holds, so a token asking to cancel
	// is told no however it is wired.
	if domain.PermissionSet(perm)&^domain.APITokenPermissions() != 0 {
		return false, nil
	}
	return a.tokensOf.Can(ctx, who, perm, projectID)
}

type cancelling struct {
	service *Service
	runs    *fakeRuns
	journal *fakeJournal
	authz   *recordingTokens
	run     domain.Run
	token   domain.Identity
}

// cancellingFixture holds one run launched by `token`, in the token's project.
func cancellingFixture(status domain.RunStatus, outcome domain.CancelOutcome) cancelling {
	token := aToken()
	launched := domain.Run{
		ID: uuid.New(), ProjectID: uuid.New(), Status: status, TriggeredByToken: &token.TokenID,
	}
	runs := &fakeRuns{byID: map[uuid.UUID]domain.Run{launched.ID: launched}, cancelled: outcome}
	journal := &fakeJournal{}
	authz := &recordingTokens{tokensOf: tokensOf{token: token.TokenID, project: launched.ProjectID}}

	service := NewService(
		runs, fakeScenarios{}, fakeEnvironments{}, &fakeAllowlist{}, noSecrets{},
		nil, nil, nil, nil, journal, nil, nil,
		authz, fixedClock{at: time.Unix(0, 0).UTC()}, Accepting(true), discardLogger(),
	)
	return cancelling{service: service, runs: runs, journal: journal, authz: authz, run: launched, token: token}
}

func TestATokenCancelsTheRunItLaunched(t *testing.T) {
	f := cancellingFixture(domain.StatusQueued, domain.RunCancelled)

	outcome, err := f.service.Cancel(context.Background(), f.token, f.run.ID)
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if outcome != domain.RunCancelled || f.runs.cancelCalls != 1 {
		t.Errorf("outcome %q after %d call(s), want cancelled after one", outcome, f.runs.cancelCalls)
	}
}

// A run being executed is asked to stop and not declared stopped, for a token as
// for a person: the status that says the load is gone is the worker's to write.
func TestATokenCancellingARunningRunOnlyAsksIt(t *testing.T) {
	f := cancellingFixture(domain.StatusRunning, domain.CancellationRequested)

	outcome, err := f.service.Cancel(context.Background(), f.token, f.run.ID)
	if err != nil || outcome != domain.CancellationRequested {
		t.Errorf("outcome %q, err %v, want cancellation_requested", outcome, err)
	}
}

// The permission a token holds is "run a test". Asking it for the permission to
// cancel would be asking a question whose answer is always no — and granting it
// that permission would let a token stop anybody's run in the project.
func TestATokenIsAskedWhetherItMayRunATestAndNeverWhetherItMayCancel(t *testing.T) {
	f := cancellingFixture(domain.StatusQueued, domain.RunCancelled)

	if _, err := f.service.Cancel(context.Background(), f.token, f.run.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	if len(f.authz.asked) == 0 {
		t.Fatal("the authorizer was never asked")
	}
	for _, perm := range f.authz.asked {
		if perm != domain.PermRunTest {
			t.Errorf("a token was asked about permission %d, want only the one to run a test", int64(perm))
		}
	}
}

func TestACancellationByATokenIsWrittenUnderTheToken(t *testing.T) {
	f := cancellingFixture(domain.StatusQueued, domain.RunCancelled)

	if _, err := f.service.Cancel(context.Background(), f.token, f.run.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	entry := only(t, f.journal)
	byThisToken(t, entry, f.token)
	if entry.Action != domain.ActionRunCancelled || entry.Outcome != domain.Allowed {
		t.Errorf("the entry says %s / %s", entry.Action, entry.Outcome)
	}
}

// --- What a token may not cancel ------------------------------------------------------

func TestATokenCannotCancelARunItDidNotLaunch(t *testing.T) {
	person := uuid.New()
	anotherToken := uuid.New()

	cases := map[string]func(*domain.Run){
		"a run a person launched":      func(r *domain.Run) { r.TriggeredByToken, r.TriggeredBy = nil, &person },
		"a run another token launched": func(r *domain.Run) { r.TriggeredByToken = &anotherToken },
		"a run nobody is recorded for": func(r *domain.Run) { r.TriggeredByToken, r.TriggeredBy = nil, nil },
	}

	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			f := cancellingFixture(domain.StatusQueued, domain.RunCancelled)
			theirs := f.run
			change(&theirs)
			f.runs.byID[theirs.ID] = theirs

			_, err := f.service.Cancel(context.Background(), f.token, theirs.ID)

			if !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("got %v, want the run to be not found", err)
			}
			if f.runs.cancelCalls != 0 {
				t.Error("a run the token did not launch was sent to the database to be cancelled")
			}

			// A refusal is in the journal, under the token.
			entry := only(t, f.journal)
			byThisToken(t, entry, f.token)
			if entry.Action != domain.ActionRunCancelled || entry.Outcome != domain.Refused {
				t.Errorf("the entry says %s / %s, want a refused cancellation", entry.Action, entry.Outcome)
			}
		})
	}
}

// The answer to a token that did not launch the run, to a token of another
// project and to somebody asking about a run that is not there are one answer.
func TestEveryRefusalOfATokenReadsAsARunThatIsNotThere(t *testing.T) {
	f := cancellingFixture(domain.StatusQueued, domain.RunCancelled)
	other := aToken()

	notTheLauncher := f.run
	otherToken := uuid.New()
	notTheLauncher.ID, notTheLauncher.TriggeredByToken = uuid.New(), &otherToken
	f.runs.byID[notTheLauncher.ID] = notTheLauncher

	_, ofAnotherProject := f.service.Cancel(context.Background(), other, f.run.ID)
	_, notLaunchedByIt := f.service.Cancel(context.Background(), f.token, notTheLauncher.ID)
	_, unknown := f.service.Cancel(context.Background(), f.token, uuid.New())

	for name, err := range map[string]error{
		"another project": ofAnotherProject, "not the launcher": notLaunchedByIt, "unknown": unknown,
	} {
		if !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%s: %v, want not found", name, err)
		}
	}
	if ofAnotherProject.Error() != notLaunchedByIt.Error() || ofAnotherProject.Error() != unknown.Error() {
		t.Errorf("the answers differ: %q, %q, %q", ofAnotherProject, notLaunchedByIt, unknown)
	}
	if f.runs.cancelCalls != 0 {
		t.Error("something was cancelled that should not have been")
	}
}

// A finished run answers as it does for a person: there is nothing to stop.
func TestATokenCancellingAFinishedRunOfItsOwnIsToldItIsOver(t *testing.T) {
	f := cancellingFixture(domain.StatusPassed, domain.RunCancelled)

	_, err := f.service.Cancel(context.Background(), f.token, f.run.ID)

	if !errors.Is(err, domain.ErrNotCancellable) || f.runs.cancelCalls != 0 {
		t.Errorf("got %v after %d call(s), want not cancellable and no write", err, f.runs.cancelCalls)
	}
}

// What a person is asked is unchanged.
func TestAPersonIsStillAskedForThePermissionToCancel(t *testing.T) {
	f := cancellingFixture(domain.StatusQueued, domain.RunCancelled)
	person := domain.Identity{OrgID: uuid.New(), UserID: uuid.New()}
	f.authz.tokensOf = tokensOf{} // nobody is a token here

	_, _ = f.service.Cancel(context.Background(), person, f.run.ID)

	for _, perm := range f.authz.asked {
		if perm != domain.PermCancelRun {
			t.Errorf("a person was asked about permission %d, want the one to cancel", int64(perm))
		}
	}
}

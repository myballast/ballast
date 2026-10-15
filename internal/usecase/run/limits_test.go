package run

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// The last line in front of a loop. Each limit refuses at its threshold, lets
// through just under it, and leaves a trace either way.

func TestARunIsRefusedWhenTheQueueIsFull(t *testing.T) {
	f := newFixture(t, nil)
	f.runs.queued = domain.MaxQueuedRunsPerOrg

	_, err := f.service.Enqueue(context.Background(), f.command())
	if !errors.Is(err, domain.ErrTooManyRuns) {
		t.Fatalf("got %v, want ErrTooManyRuns", err)
	}
	if f.runs.created.ScenarioID != uuid.Nil {
		t.Error("a run was written although the queue was full")
	}

	// And the journal says which limit, with the figure reached: one refusal
	// is somebody busy, a page of them is somebody looping.
	entry := only(t, f.audit)
	switch {
	case entry.Action != domain.ActionRunRefused || entry.Outcome != domain.Refused:
		t.Errorf("the entry says %s / %s", entry.Action, entry.Outcome)
	case entry.Details["limit"] != "queued":
		t.Errorf("the entry blames %q", entry.Details["limit"])
	case entry.Details["reached"] != "20":
		t.Errorf("the entry reached %q", entry.Details["reached"])
	}
}

// One below the threshold is still allowed. A limit that refused at the last
// permitted run would be a limit of one less than it says.
func TestTheLastPermittedRunStillGoesThrough(t *testing.T) {
	f := newFixture(t, nil)
	f.runs.queued = domain.MaxQueuedRunsPerOrg - 1
	f.runs.triggered = Triggered{
		ByUser: domain.MaxRunsPerUserPerWindow - 1,
		ByOrg:  domain.MaxRunsPerOrgPerWindow - 1,
	}

	if _, err := f.service.Enqueue(context.Background(), f.command()); err != nil {
		t.Fatalf("the last permitted run was refused: %v", err)
	}
}

func TestAPersonIsRefusedOnTheirOwnRate(t *testing.T) {
	f := newFixture(t, nil)
	f.runs.triggered = Triggered{ByUser: domain.MaxRunsPerUserPerWindow, ByOrg: 1}

	_, err := f.service.Enqueue(context.Background(), f.command())
	if !errors.Is(err, domain.ErrTooManyRuns) {
		t.Fatalf("got %v, want ErrTooManyRuns", err)
	}

	if limit := only(t, f.audit).Details["limit"]; limit != "triggered_by_user" {
		t.Fatalf("the entry blames %q", limit)
	}
}

// An organization is refused on its own total even when the person asking has
// triggered nothing: one quiet person in a busy organization is still part of
// it.
func TestAnOrganizationIsRefusedOnItsOwnRate(t *testing.T) {
	f := newFixture(t, nil)
	f.runs.triggered = Triggered{ByUser: 0, ByOrg: domain.MaxRunsPerOrgPerWindow}

	_, err := f.service.Enqueue(context.Background(), f.command())
	if !errors.Is(err, domain.ErrTooManyRuns) {
		t.Fatalf("got %v, want ErrTooManyRuns", err)
	}

	if limit := only(t, f.audit).Details["limit"]; limit != "triggered_by_org" {
		t.Fatalf("the entry blames %q", limit)
	}
}

// What a refused caller is told: come back when the oldest run of the window
// leaves it, which is one window after it was created.
func TestARefusedCallerIsToldWhenToComeBack(t *testing.T) {
	f := newFixture(t, nil)
	f.runs.triggered = Triggered{ByUser: domain.MaxRunsPerUserPerWindow}
	// The oldest run of the window was created fifty minutes ago, so ten
	// minutes of the hour are left.
	f.runs.oldest = time.Unix(0, 0).UTC().Add(-50 * time.Minute)
	f.runs.oldestFound = true

	_, err := f.service.Enqueue(context.Background(), f.command())

	var refusal TooManyRuns
	if !errors.As(err, &refusal) {
		t.Fatalf("got %v, want a TooManyRuns", err)
	}
	if refusal.RetryAfter != 10*time.Minute {
		t.Fatalf("the caller was told to come back in %s", refusal.RetryAfter)
	}
}

// A figure that cannot be read is no figure: telling somebody to come back in
// a minute when the truth is an hour is worse than telling them nothing.
func TestNoPromiseIsMadeWhenTheWaitCannotBeKnown(t *testing.T) {
	f := newFixture(t, nil)
	f.runs.triggered = Triggered{ByUser: domain.MaxRunsPerUserPerWindow}
	f.runs.oldestErr = errors.New("the database is not answering")

	_, err := f.service.Enqueue(context.Background(), f.command())

	var refusal TooManyRuns
	if !errors.As(err, &refusal) {
		t.Fatalf("got %v, want a TooManyRuns", err)
	}
	if refusal.RetryAfter != 0 {
		t.Fatalf("a wait of %s was promised on no evidence", refusal.RetryAfter)
	}
}

// A queue that is full empties when runs finish, and nobody knows when that
// is. Saying so is better than a number made up to fill the header.
func TestAFullQueuePromisesNothing(t *testing.T) {
	f := newFixture(t, nil)
	f.runs.queued = domain.MaxQueuedRunsPerOrg
	f.runs.oldest, f.runs.oldestFound = time.Unix(0, 0).UTC(), true

	_, err := f.service.Enqueue(context.Background(), f.command())

	var refusal TooManyRuns
	if !errors.As(err, &refusal) {
		t.Fatalf("got %v, want a TooManyRuns", err)
	}
	if refusal.RetryAfter != 0 {
		t.Fatalf("a full queue promised a slot in %s", refusal.RetryAfter)
	}
}

// The window asked about is the one the limits name, counted back from now.
func TestTheWindowIsTheOneTheLimitsName(t *testing.T) {
	f := newFixture(t, nil)
	now := time.Unix(0, 0).UTC()

	if _, err := f.service.Enqueue(context.Background(), f.command()); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if want := now.Add(-domain.RateWindow); !f.runs.triggeredSince.Equal(want) {
		t.Fatalf("the runs counted were those since %s, wanted %s", f.runs.triggeredSince, want)
	}
}

// The limits are asked before anything about the request is read: somebody
// past their limit is past it whatever they were going to ask for.
func TestTheLimitsAreAskedBeforeTheScenarioIsRead(t *testing.T) {
	f := newFixture(t, nil)
	f.runs.queued = domain.MaxQueuedRunsPerOrg

	if _, err := f.service.Enqueue(context.Background(), f.command()); err == nil {
		t.Fatal("the run was accepted")
	}

	if strings.Contains(f.runs.created.ScenarioID.String(), "-") &&
		f.runs.created.ScenarioID != uuid.Nil {
		t.Error("the scenario was read before the limits were asked")
	}
}

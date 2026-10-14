package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// The four things the journal promises about every entry: who, what target,
// what result, and when.
func TestAnEntryCarriesAuthorTargetOutcomeAndDate(t *testing.T) {
	var (
		org     = uuid.New()
		actor   = uuid.New()
		project = uuid.New()
		run     = uuid.New()
		at      = time.Date(2026, 10, 14, 9, 30, 0, 0, time.UTC)
	)

	entry := Allow(org, &actor, ActionRunQueued, TargetRun, at).
		InProject(project).
		On(run).
		With("profile", "smoke")

	switch {
	case entry.ActorID == nil || *entry.ActorID != actor:
		t.Errorf("the entry names %v as its author", entry.ActorID)
	case entry.TargetType != TargetRun || entry.TargetID == nil || *entry.TargetID != run:
		t.Errorf("the entry's target is %s %v", entry.TargetType, entry.TargetID)
	case entry.Outcome != Allowed:
		t.Errorf("the outcome is %q", entry.Outcome)
	case !entry.At.Equal(at):
		t.Errorf("the entry is dated %s", entry.At)
	case entry.ProjectID == nil || *entry.ProjectID != project:
		t.Errorf("the entry's project is %v", entry.ProjectID)
	case entry.Details["profile"] != "smoke":
		t.Errorf("the details are %v", entry.Details)
	}
}

// Ballast refusing a run hours after somebody queued it acted on nobody's
// behalf. A journal that invented an author there would be lying about the one
// thing it exists to record.
func TestAnEntryWithNoPersonBehindItHasNoAuthor(t *testing.T) {
	entry := Refuse(uuid.New(), nil, ActionRunRefused, TargetRun, "the window closed", time.Now())

	if entry.ActorID != nil {
		t.Fatalf("an entry Ballast wrote names %v as its author", entry.ActorID)
	}
	if entry.Outcome != Refused {
		t.Fatalf("the outcome is %q", entry.Outcome)
	}
	if entry.Details["reason"] != "the window closed" {
		t.Fatalf("the details are %v", entry.Details)
	}
}

// An action on the whole organization belongs to no project, and must not be
// given one: an emergency stop filed under a project would read as "somebody
// stopped that project's runs".
func TestAnActionOnTheOrganizationBelongsToNoProject(t *testing.T) {
	entry := Allow(uuid.New(), nil, ActionRunsStopped, TargetOrg, time.Now())

	if entry.ProjectID != nil {
		t.Fatalf("an organization-wide action was filed under project %v", entry.ProjectID)
	}
	if entry.TargetID != nil {
		t.Fatalf("an action with no single target names %v", entry.TargetID)
	}
}

// The builders return a copy. Two entries built from one base must not share a
// map: recording one run's details would otherwise edit another's entry.
func TestBuildingOneEntryDoesNotEditAnother(t *testing.T) {
	base := Allow(uuid.New(), nil, ActionRunQueued, TargetRun, time.Now()).With("profile", "smoke")

	first := base.With("vus", "10")
	second := base.With("vus", "500")

	switch {
	case first.Details["vus"] != "10":
		t.Errorf("the first entry says %q", first.Details["vus"])
	case second.Details["vus"] != "500":
		t.Errorf("the second entry says %q", second.Details["vus"])
	case len(base.Details) != 1:
		t.Errorf("the entry they were built from now says %v", base.Details)
	}
}

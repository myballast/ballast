package run

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// usageFixture is a run service whose repository answers with one figure.
func usageFixture(authz Authorizer, projects ...uuid.UUID) (*Service, *fakeRuns) {
	runs := &fakeRuns{usage: domain.Usage{EgressBytes: 4096, Runs: 3, UnmeasuredRuns: 1}}

	return NewService(
		runs, fakeScenarios{}, fakeEnvironments{}, &fakeAllowlist{}, noSecrets{},
		nil, nil,
		&fakeProjects{ids: projects},
		nil, &fakeJournal{}, nil, nil,
		authz,
		fixedClock{at: time.Unix(0, 0).UTC()},
		Accepting(true),
		discardLogger(),
	), runs
}

// An organization's figure is the whole organization's to read. Somebody who
// administers part of it is refused rather than handed a total that silently
// leaves out what they cannot see: a partial number read as a total is worse
// than no number at all.
func TestAnOrganizationsUsageIsRefusedToAPartialAdministrator(t *testing.T) {
	theirs, somebodyElses := uuid.New(), uuid.New()
	service, runs := usageFixture(
		&adminOf{projects: map[uuid.UUID]bool{theirs: true}}, theirs, somebodyElses)

	from, to, _ := domain.ParseMonth("2026-10")
	_, err := service.UsageInMonth(context.Background(), someone(), from, to)
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("got %v, want ErrForbidden", err)
	}
	if runs.usageAsked != 0 {
		t.Fatal("the figure was read for somebody who may not read it")
	}
}

func TestAnAdministratorOfEverythingReadsTheOrganizationsUsage(t *testing.T) {
	first, second := uuid.New(), uuid.New()
	service, runs := usageFixture(
		&adminOf{projects: map[uuid.UUID]bool{first: true, second: true}}, first, second)

	from, to, _ := domain.ParseMonth("2026-10")
	usage, err := service.UsageInMonth(context.Background(), someone(), from, to)
	if err != nil {
		t.Fatalf("UsageInMonth: %v", err)
	}

	switch {
	case usage.EgressBytes != 4096:
		t.Errorf("the figure read as %+v", usage)
	case !runs.askedFrom.Equal(from) || !runs.askedTo.Equal(to):
		t.Errorf("the period asked for was %s to %s", runs.askedFrom, runs.askedTo)
	}
}

// A project's figure is its administrator's. Running tests in a project is not
// the same as answering for what it costs.
func TestAProjectsUsageIsRefusedToSomebodyWhoDoesNotAdministerIt(t *testing.T) {
	project := uuid.New()
	service, runs := usageFixture(&adminOf{projects: map[uuid.UUID]bool{}}, project)

	from, to, _ := domain.ParseMonth("2026-10")
	_, err := service.ProjectUsageInMonth(context.Background(), someone(), project, from, to)
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("got %v, want ErrForbidden", err)
	}
	if runs.usageAsked != 0 {
		t.Fatal("the figure was read for somebody who may not read it")
	}
}

// And the permission asked about is an administrator's, on the project that was
// asked about. The right permission on the wrong project protects nothing.
func TestAProjectsUsageAsksAboutThatProject(t *testing.T) {
	project := uuid.New()
	authz := &askedForUsage{allow: true}
	runs := &fakeRuns{usage: domain.Usage{EgressBytes: 10}}

	service := NewService(
		runs, fakeScenarios{}, fakeEnvironments{}, &fakeAllowlist{}, noSecrets{},
		nil, nil, &fakeProjects{}, nil, &fakeJournal{}, nil, nil,
		authz, fixedClock{at: time.Unix(0, 0).UTC()}, Accepting(true), discardLogger(),
	)

	from, to, _ := domain.ParseMonth("2026-10")
	if _, err := service.ProjectUsageInMonth(
		context.Background(), someone(), project, from, to,
	); err != nil {
		t.Fatalf("ProjectUsageInMonth: %v", err)
	}

	switch {
	case authz.perm != domain.PermManageProject:
		t.Errorf("the reading asked for %v", authz.perm)
	case authz.projectID != project:
		t.Errorf("the reading asked about project %s, wanted %s", authz.projectID, project)
	case runs.askedProject != project:
		t.Errorf("the repository was asked about project %s", runs.askedProject)
	}
}

// askedForUsage records what the authorizer was questioned on.
type askedForUsage struct {
	allow     bool
	perm      domain.Permission
	projectID uuid.UUID
}

func (a *askedForUsage) Can(
	_ context.Context, _ domain.Identity, perm domain.Permission, projectID uuid.UUID,
) (bool, error) {
	a.perm, a.projectID = perm, projectID
	return a.allow, nil
}

// What a run moved reaches the store along with what it measured, and the two
// figures are kept apart: one is what left the machine, the other what the
// script said it sent.
func TestBothMeasurementsOfWhatARunMovedAreStored(t *testing.T) {
	f := newExecutionFixture()
	f.parser.results = domain.RunResults{
		Traffic: domain.Traffic{SentBytes: 1000, ReceivedBytes: 20000, Measured: true},
	}
	f.executor.outcome = Outcome{
		Summary: []byte(`{"metrics":{}}`),
		Egress:  domain.Traffic{SentBytes: 1500, ReceivedBytes: 21000, Measured: true},
	}

	f.process(t)

	if f.runs.saved.Traffic.SentBytes != 1000 {
		t.Errorf("the generator's figure was stored as %+v", f.runs.saved.Traffic)
	}
	if f.runs.savedEgress.SentBytes != 1500 || f.runs.savedEgress.ReceivedBytes != 21000 {
		t.Errorf("the wire figure was stored as %+v", f.runs.savedEgress)
	}
	if !f.runs.savedEgress.Measured {
		t.Error("a run both measurements were taken of was stored as unmeasured")
	}
}

// A run whose proxy could not be read is unmeasured, not free.
func TestARunWhoseWireFigureIsMissingIsStoredAsUnmeasured(t *testing.T) {
	f := newExecutionFixture()
	f.parser.results = domain.RunResults{
		Traffic: domain.Traffic{SentBytes: 1000, ReceivedBytes: 20000, Measured: true},
	}
	f.executor.outcome = Outcome{Summary: []byte(`{"metrics":{}}`)}

	f.process(t)

	if f.runs.savedEgress.Measured {
		t.Fatal("a run nobody counted on the wire was stored as measured")
	}
	if f.runs.savedEgress.SentBytes != 0 {
		t.Fatalf("an unmeasured run was stored with %d bytes sent", f.runs.savedEgress.SentBytes)
	}
}

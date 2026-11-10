package runner

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// orgDirectory is the list of organizations a worker may be asked to serve, and how
// often it was read.
type orgDirectory struct {
	withWork []uuid.UUID
	all      []uuid.UUID
	err      error
	calls    int
}

func (d *orgDirectory) WithWork(context.Context) ([]uuid.UUID, error) {
	d.calls++
	return d.withWork, d.err
}

func (d *orgDirectory) All(context.Context) ([]uuid.UUID, error) {
	d.calls++
	return d.all, d.err
}

// A worker pointed at one organization behaves as it did before there was any
// other way to run one: it serves that organization and nothing else. It is built
// from the organization alone: a restricted worker needs neither the list nor the
// privilege to read it.
func TestAWorkerRestrictedToOneOrganizationServesOnlyThatOne(t *testing.T) {
	org := uuid.New()
	scope := OnlyOrganization(org)

	active, err := scope.Active(context.Background())
	if err != nil {
		t.Fatalf("active: %v", err)
	}
	present, err := scope.Present(context.Background())
	if err != nil {
		t.Fatalf("present: %v", err)
	}

	if len(active) != 1 || active[0] != org || len(present) != 1 || present[0] != org {
		t.Fatalf("a restricted worker serves %v and announces in %v, want only %s", active, present, org)
	}
}

func TestAWorkerForEveryOrganizationServesTheOnesWithWork(t *testing.T) {
	busy, idle := uuid.New(), uuid.New()
	dir := &orgDirectory{withWork: []uuid.UUID{busy}, all: []uuid.UUID{busy, idle}}
	scope := AllOrganizations(dir)

	active, err := scope.Active(context.Background())
	if err != nil {
		t.Fatalf("active: %v", err)
	}
	if len(active) != 1 || active[0] != busy {
		t.Fatalf("active = %v, want only the organization with work (%s)", active, busy)
	}
}

// An organization with nothing queued still has to see its worker online, so
// the presence is written to every one, not only to the ones with work.
func TestAWorkerForEveryOrganizationAnnouncesInAllOfThem(t *testing.T) {
	busy, idle := uuid.New(), uuid.New()
	dir := &orgDirectory{withWork: []uuid.UUID{busy}, all: []uuid.UUID{busy, idle}}
	scope := AllOrganizations(dir)

	present, err := scope.Present(context.Background())
	if err != nil {
		t.Fatalf("present: %v", err)
	}
	if len(present) != 2 {
		t.Fatalf("announced in %v, want both organizations", present)
	}
}

// A list that could not be read is an error, never an empty list: a worker that
// reads "no organizations" from a failure would sweep as if nobody had a run.
func TestAListThatCannotBeReadIsAnErrorNotAnEmptyList(t *testing.T) {
	dir := &orgDirectory{err: errors.New("the database is gone")}
	scope := AllOrganizations(dir)

	if _, err := scope.Active(context.Background()); err == nil {
		t.Fatal("a failed read of the organizations with work was swallowed")
	}
	if _, err := scope.Present(context.Background()); err == nil {
		t.Fatal("a failed read of the organizations was swallowed")
	}
}

package run

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// A pipeline reads its run with the token that launched it: the page of the run
// and its output. What these tests hold in place is the acceptance criterion
// that a token of another project learns nothing — the answer is the one a
// person without rights gets, word for word — and that the token's own project
// reads as usual.

// tokensOf answers like the real authorizer does for a token: the one token it
// knows holds the permission in its own project and in no other, and any other
// token holds nothing.
type tokensOf struct{ token, project uuid.UUID }

func (a tokensOf) Can(
	_ context.Context, who domain.Identity, _ domain.Permission, projectID uuid.UUID,
) (bool, error) {
	return who.TokenID != uuid.Nil && who.TokenID == a.token && projectID == a.project, nil
}

// readingFixture is a service holding one run, readable by the token it returns,
// which belongs to the run's project, and by nobody else.
func readingFixture(status domain.RunStatus) (*Service, domain.Run, domain.Identity) {
	existing := domain.Run{ID: uuid.New(), ProjectID: uuid.New(), Status: status}
	logs := &fakeLogs{journal: &journal{}, since: wrote("starting", "done")}
	own := aToken()

	service := NewService(
		&fakeRuns{byID: map[uuid.UUID]domain.Run{existing.ID: existing}},
		fakeScenarios{}, fakeEnvironments{}, &fakeAllowlist{}, noSecrets{},
		nil, nil, nil, logs, &fakeJournal{}, nil, nil,
		tokensOf{token: own.TokenID, project: existing.ProjectID},
		fixedClock{at: time.Unix(0, 0).UTC()}, Accepting(true), discardLogger(),
	)
	return service, existing, own
}

func aToken() domain.Identity {
	return domain.Identity{OrgID: uuid.New(), TokenID: uuid.New()}
}

func TestATokenOfTheProjectReadsTheRun(t *testing.T) {
	service, existing, own := readingFixture(domain.StatusRunning)

	detail, _, err := service.Detail(context.Background(), own, existing.ID)
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if detail.ID != existing.ID {
		t.Errorf("read run %s, want %s", detail.ID, existing.ID)
	}

	lines, status, err := service.LogsSince(context.Background(), own, existing.ID, -1, 0)
	if err != nil {
		t.Fatalf("LogsSince: %v", err)
	}
	if len(lines) != 2 || status != domain.StatusRunning {
		t.Errorf("read %d line(s) with status %s, want 2 and running", len(lines), status)
	}
}

// The answer to a token of another project is the answer to somebody who may not
// see the run, and the answer to somebody asking about a run that does not exist.
// Three situations, one reply: anything else would confirm that the identifier
// stands for something.
func TestATokenOfAnotherProjectIsToldTheRunIsNotThere(t *testing.T) {
	service, existing, _ := readingFixture(domain.StatusPassed)
	elsewhere := aToken()
	person := domain.Identity{OrgID: uuid.New(), UserID: uuid.New()}

	read := map[string]func(who domain.Identity, id uuid.UUID) error{
		"the run": func(who domain.Identity, id uuid.UUID) error {
			_, _, err := service.Detail(context.Background(), who, id)
			return err
		},
		"its output": func(who domain.Identity, id uuid.UUID) error {
			_, _, err := service.LogsSince(context.Background(), who, id, -1, 0)
			return err
		},
	}

	for what, ask := range read {
		t.Run(what, func(t *testing.T) {
			byToken := ask(elsewhere, existing.ID)
			byPerson := ask(person, existing.ID)
			unknown := ask(elsewhere, uuid.New())

			for name, err := range map[string]error{"token": byToken, "person": byPerson, "unknown run": unknown} {
				if !errors.Is(err, domain.ErrNotFound) {
					t.Errorf("%s: %v, want not found", name, err)
				}
			}
			if byToken.Error() != byPerson.Error() || byToken.Error() != unknown.Error() {
				t.Errorf("the three answers differ: %q, %q, %q", byToken, byPerson, unknown)
			}
		})
	}
}

// Nothing of the run is read once the permission has been refused: not its
// output either.
func TestAnotherProjectsTokenReadsNoLines(t *testing.T) {
	service, existing, _ := readingFixture(domain.StatusRunning)

	lines, status, err := service.LogsSince(context.Background(), aToken(), existing.ID, -1, 0)

	if !errors.Is(err, domain.ErrNotFound) || len(lines) != 0 || status != "" {
		t.Fatalf("got %d line(s), status %q, err %v", len(lines), status, err)
	}
}

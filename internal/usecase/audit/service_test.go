package audit

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// fakeJournal records what it was asked for and hands back what the test set up.
type fakeJournal struct {
	entries   []domain.AuditEntry
	err       error
	orgID     uuid.UUID
	projectID uuid.UUID
	limit     int
	asked     int
}

func (f *fakeJournal) InProject(
	_ context.Context, orgID, projectID uuid.UUID, limit int,
) ([]domain.AuditEntry, error) {
	f.asked++
	f.orgID, f.projectID, f.limit = orgID, projectID, limit
	return f.entries, f.err
}

// askedAbout records the permission and project the authorizer was questioned
// on, so a test can check the scope rather than only the answer.
type askedAbout struct {
	allow     bool
	perm      domain.Permission
	projectID uuid.UUID
}

func (a *askedAbout) Can(
	_ context.Context, _ domain.Identity, perm domain.Permission, projectID uuid.UUID,
) (bool, error) {
	a.perm, a.projectID = perm, projectID
	return a.allow, nil
}

func discard() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func someone() domain.Identity {
	return domain.Identity{UserID: uuid.New(), OrgID: uuid.New()}
}

// The journal's own lock, independent of the one the router's table imposes.
// Two locks on the same door is the arrangement everywhere else in this
// project, and it is what keeps a route registered by hand from being open.
func TestTheJournalIsRefusedToSomebodyWhoIsNotAnAdministrator(t *testing.T) {
	journal := &fakeJournal{entries: []domain.AuditEntry{{Action: domain.ActionRunQueued}}}
	service := NewService(journal, &askedAbout{allow: false}, discard())

	_, err := service.InProject(context.Background(), someone(), uuid.New(), 0)
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("got %v, want ErrForbidden", err)
	}
	if journal.asked != 0 {
		t.Fatal("the journal was read for somebody who may not read it")
	}
}

// The permission asked about is the one no mask but an administrator's holds,
// and it is asked about the project whose journal was requested. A check with
// the right permission on the wrong project protects nothing.
func TestTheJournalAsksForAnAdministratorsPermissionOnThatProject(t *testing.T) {
	authz := &askedAbout{allow: true}
	service := NewService(&fakeJournal{}, authz, discard())

	project := uuid.New()
	if _, err := service.InProject(context.Background(), someone(), project, 0); err != nil {
		t.Fatalf("InProject: %v", err)
	}

	if authz.perm != domain.PermManageProject {
		t.Errorf("the journal asked for %v", authz.perm)
	}
	if authz.projectID != project {
		t.Errorf("the journal asked about project %s, wanted %s", authz.projectID, project)
	}
}

// What the caller asked for reaches the store, including the organization —
// which comes from the session and never from the request.
func TestTheReadingIsScopedToTheCallersOrganization(t *testing.T) {
	journal := &fakeJournal{}
	service := NewService(journal, &askedAbout{allow: true}, discard())

	who := someone()
	project := uuid.New()
	if _, err := service.InProject(context.Background(), who, project, 25); err != nil {
		t.Fatalf("InProject: %v", err)
	}

	switch {
	case journal.orgID != who.OrgID:
		t.Errorf("the journal of organization %s was read, the caller's is %s", journal.orgID, who.OrgID)
	case journal.projectID != project:
		t.Errorf("the journal of project %s was read", journal.projectID)
	case journal.limit != 25:
		t.Errorf("the page asked for was %d", journal.limit)
	}
}

func TestTheEntriesComeBackAsTheyWereRead(t *testing.T) {
	at := time.Date(2026, 10, 14, 11, 0, 0, 0, time.UTC)
	actor := uuid.New()
	journal := &fakeJournal{entries: []domain.AuditEntry{
		domain.Allow(uuid.New(), &actor, domain.ActionTargetApproved, domain.TargetEnvironment, at),
	}}
	service := NewService(journal, &askedAbout{allow: true}, discard())

	entries, err := service.InProject(context.Background(), someone(), uuid.New(), 0)
	if err != nil {
		t.Fatalf("InProject: %v", err)
	}
	if len(entries) != 1 || entries[0].Action != domain.ActionTargetApproved || !entries[0].At.Equal(at) {
		t.Fatalf("the reading returned %+v", entries)
	}
}

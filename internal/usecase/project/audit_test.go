package project

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// A role granted with nobody's name on it is indistinguishable from one nobody
// gave, so the entry travels into the transaction that grants it rather than
// being written beside it. What a test reads here is what that transaction was
// handed.

func TestGrantingARoleCarriesItsEntryIntoTheTransaction(t *testing.T) {
	memberships := &fakeMemberships{members: map[uuid.UUID][]Member{}}
	service := newService(&fakeProjects{byID: map[uuid.UUID]domain.Project{}}, memberships, &allowAll{})

	var (
		who     = someone()
		project = uuid.New()
		member  = uuid.New()
	)

	if err := service.SetMemberRole(
		context.Background(), who, project, member, domain.RoleRunner,
	); err != nil {
		t.Fatalf("SetMemberRole: %v", err)
	}

	if len(memberships.recorded) != 1 {
		t.Fatalf("the change carried %d entries", len(memberships.recorded))
	}

	entry := memberships.recorded[0]
	switch {
	case entry.Action != domain.ActionRoleChanged || entry.Outcome != domain.Allowed:
		t.Errorf("the entry says %s / %s", entry.Action, entry.Outcome)
	case entry.ActorID == nil || *entry.ActorID != who.UserID:
		t.Errorf("the author is %v", entry.ActorID)
	// The target is the person whose rights changed, not the person who
	// changed them: a journal answers "who can do what, and since when".
	case entry.TargetType != domain.TargetMembership || entry.TargetID == nil || *entry.TargetID != member:
		t.Errorf("the target is %s %v", entry.TargetType, entry.TargetID)
	case entry.ProjectID == nil || *entry.ProjectID != project:
		t.Errorf("the project is %v", entry.ProjectID)
	case entry.At.IsZero():
		t.Error("the entry has no date")
	case entry.Details["role"] != string(domain.RoleRunner):
		t.Errorf("the entry does not say which role: %v", entry.Details)
	}
}

func TestRemovingAMemberCarriesItsEntryIntoTheTransaction(t *testing.T) {
	var (
		who           = someone()
		project       = uuid.New()
		member        = uuid.New()
		administrator = uuid.New()
	)

	// Somebody else administers the project, so removing this member is not
	// refused for leaving it without one.
	memberships := &fakeMemberships{members: map[uuid.UUID][]Member{
		project: {admin(project, administrator), viewer(project, member)},
	}}
	service := newService(&fakeProjects{byID: map[uuid.UUID]domain.Project{}}, memberships, &allowAll{})

	if err := service.RemoveMember(context.Background(), who, project, member); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}

	if len(memberships.recorded) != 1 {
		t.Fatalf("the change carried %d entries", len(memberships.recorded))
	}

	entry := memberships.recorded[0]
	switch {
	case entry.Action != domain.ActionMemberRemoved || entry.Outcome != domain.Allowed:
		t.Errorf("the entry says %s / %s", entry.Action, entry.Outcome)
	case entry.TargetID == nil || *entry.TargetID != member:
		t.Errorf("the target is %v", entry.TargetID)
	case entry.ProjectID == nil || *entry.ProjectID != project:
		t.Errorf("the project is %v", entry.ProjectID)
	}
}

// A change that is refused writes nothing, because nothing is written: the
// entry lives in the transaction that would have performed it.
func TestARefusedRoleChangeCarriesNothing(t *testing.T) {
	memberships := &fakeMemberships{members: map[uuid.UUID][]Member{}}
	service := newService(&fakeProjects{byID: map[uuid.UUID]domain.Project{}}, memberships, refuseAll{})

	if err := service.SetMemberRole(
		context.Background(), someone(), uuid.New(), uuid.New(), domain.RoleRunner,
	); err == nil {
		t.Fatal("the role was granted")
	}

	if len(memberships.recorded) != 0 {
		t.Fatalf("a refused change carried %+v", memberships.recorded)
	}
}

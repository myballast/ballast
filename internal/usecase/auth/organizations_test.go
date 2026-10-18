package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// accountsOf is one person known in several organizations, as a different user
// in each, the way the schema keeps them.
type accountsOf struct {
	subject string
	known   []Me
}

func (a accountsOf) Lookup(_ context.Context, subject string) ([]domain.Identity, error) {
	if subject != a.subject {
		return nil, nil
	}
	found := make([]domain.Identity, 0, len(a.known))
	for _, me := range a.known {
		found = append(found, domain.Identity{UserID: me.UserID, OrgID: me.OrgID})
	}
	return found, nil
}

func (a accountsOf) Me(_ context.Context, who domain.Identity) (Me, error) {
	for _, me := range a.known {
		if me.UserID == who.UserID && me.OrgID == who.OrgID {
			return me, nil
		}
	}
	return Me{}, domain.ErrNotFound
}

func (accountsOf) Create(context.Context, NewAccount) error { return nil }

func twoOrganizations() (accountsOf, Me, Me) {
	home := Me{UserID: uuid.New(), OrgID: uuid.New(), OrgName: "Ada's organization", Subject: "github|1"}
	acme := Me{UserID: uuid.New(), OrgID: uuid.New(), OrgName: "Acme", Subject: "github|1"}
	return accountsOf{subject: "github|1", known: []Me{home, acme}}, home, acme
}

func TestAPersonSeesEveryOrganizationTheyBelongTo(t *testing.T) {
	accounts, home, acme := twoOrganizations()

	got, err := newService(accounts, nil).Organizations(context.Background(), domain.Identity{UserID: home.UserID, OrgID: home.OrgID})
	if err != nil {
		t.Fatalf("Organizations: %v", err)
	}

	if len(got) != 2 || got[0].ID != home.OrgID || got[0].Name != "Ada's organization" || got[1].ID != acme.OrgID || got[1].Name != "Acme" {
		t.Errorf("got %+v", got)
	}
}

func TestSwitchingGivesTheUserOfThatOrganization(t *testing.T) {
	accounts, home, acme := twoOrganizations()

	switched, err := newService(accounts, nil).SwitchOrganization(
		context.Background(), domain.Identity{UserID: home.UserID, OrgID: home.OrgID}, acme.OrgID)
	if err != nil {
		t.Fatalf("SwitchOrganization: %v", err)
	}

	// The user there, not the one they were: a person is a different user in
	// each organization.
	if switched.OrgID != acme.OrgID || switched.UserID != acme.UserID {
		t.Errorf("switched to %+v, want org %s user %s", switched, acme.OrgID, acme.UserID)
	}
}

func TestSwitchingToAnOrganizationOfSomebodyElseIsRefused(t *testing.T) {
	accounts, home, _ := twoOrganizations()

	_, err := newService(accounts, nil).SwitchOrganization(
		context.Background(), domain.Identity{UserID: home.UserID, OrgID: home.OrgID}, uuid.New())
	if !errors.Is(err, ErrNotAMember) {
		t.Fatalf("got %v", err)
	}
}

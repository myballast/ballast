package domain

import (
	"errors"
	"testing"
	"time"
)

func TestAnAddressIsKeptInLowerCaseAndNothingElse(t *testing.T) {
	got, err := ValidateInvitation("  Ada.Lovelace@Example.TEST ", RoleRunner)
	if err != nil {
		t.Fatalf("ValidateInvitation: %v", err)
	}
	if got != "ada.lovelace@example.test" {
		t.Errorf("kept as %q", got)
	}
}

func TestWhatIsNotAnAddressOrNotARoleIsRefused(t *testing.T) {
	for _, email := range []string{"", "ada", "ada@", "@example.test", "Ada <ada@example.test>", "a@b.test, c@d.test", "a b@example.test"} {
		if _, err := ValidateInvitation(email, RoleViewer); !errors.Is(err, ErrInvalidInvitation) {
			t.Errorf("address %q: got %v", email, err)
		}
	}
	for _, role := range []Role{"", "owner", "Admin"} {
		if _, err := ValidateInvitation("ada@example.test", role); !errors.Is(err, ErrInvalidInvitation) {
			t.Errorf("role %q: got %v", role, err)
		}
	}
}

func TestAnInvitationIsOnlyPendingUntilSomethingHappensToIt(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	later, earlier := now.Add(time.Hour), now.Add(-time.Hour)

	cases := []struct {
		name string
		in   Invitation
		want InvitationState
	}{
		{"fresh", Invitation{ExpiresAt: later}, InvitationPending},
		{"past its date", Invitation{ExpiresAt: earlier}, InvitationExpired},
		// The instant of expiry is already too late: a link is good for a
		// stretch that ends, not one that includes its last moment.
		{"at its date", Invitation{ExpiresAt: now}, InvitationExpired},
		{"revoked", Invitation{ExpiresAt: later, RevokedAt: &earlier}, InvitationRevoked},
		{"used", Invitation{ExpiresAt: later, AcceptedAt: &earlier}, InvitationAccepted},
		// Used, then past its date: it was still used.
		{"used and old", Invitation{ExpiresAt: earlier, AcceptedAt: &earlier}, InvitationAccepted},
		{"revoked and old", Invitation{ExpiresAt: earlier, RevokedAt: &earlier}, InvitationRevoked},
	}
	for _, c := range cases {
		if got := c.in.State(now); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

func TestNobodyGrantsAboveTheirOwnRole(t *testing.T) {
	roles := Roles()
	for i, actor := range roles {
		for j, target := range roles {
			if got, want := CanGrant(actor, target), j <= i; got != want {
				t.Errorf("%s granting %s: %v, want %v", actor, target, got, want)
			}
		}
	}
	if CanGrant("owner", RoleViewer) || CanGrant(RoleAdmin, "owner") {
		t.Error("a role Ballast does not know took part in a grant")
	}
}

func TestSameAddressIgnoresCaseAndPadding(t *testing.T) {
	if !SameAddress(" Ada@Example.test", "ada@example.test ") {
		t.Error("the same address was told apart")
	}
	if SameAddress("ada@example.test", "grace@example.test") {
		t.Error("two addresses were taken for one")
	}
}

// The matrix on screen is read from the same masks the checks use. Pinned here
// against the design's table, row by row, so that a permission moved between
// roles shows up as a failing test rather than as a table that has quietly
// stopped matching.
func TestTheMatrixSaysWhatEachRoleCanDo(t *testing.T) {
	want := map[string][]Role{
		"view_runs":           {RoleViewer, RoleRunner, RoleMaintainer, RoleAdmin},
		"run_test":            {RoleRunner, RoleMaintainer, RoleAdmin},
		"edit_scenarios":      {RoleMaintainer, RoleAdmin},
		"manage_environments": {RoleMaintainer, RoleAdmin},
		"verify_domain":       {RoleAdmin},
		"manage_members":      {RoleAdmin},
	}

	rows := Capabilities()
	if len(rows) != len(want) {
		t.Fatalf("%d rows, want %d", len(rows), len(want))
	}
	for _, row := range rows {
		holders := map[Role]bool{}
		for _, role := range want[row.ID] {
			holders[role] = true
		}
		for _, role := range Roles() {
			if got := row.Grants(role); got != holders[role] {
				t.Errorf("%s / %s: %v, want %v", row.ID, role, got, holders[role])
			}
		}
	}
}

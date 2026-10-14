//go:build integration

// The journal, through the API that fills it.
//
// The ticket's acceptance criterion is "each of these actions produces an entry
// with author, target, result and date", so each of them is performed here the
// way a person performs it — an HTTP request with a session — and the journal
// is read back the way a person reads it.
package e2e

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// entry is one line of the journal as the API returns it.
type entry struct {
	Action     string            `json:"action"`
	Outcome    string            `json:"outcome"`
	ActorID    string            `json:"actor_id"`
	ProjectID  string            `json:"project_id"`
	TargetType string            `json:"target_type"`
	TargetID   string            `json:"target_id"`
	Details    map[string]string `json:"details"`
	At         time.Time         `json:"at"`
}

// of returns the entries for one action, in the order they were written.
func of(entries []entry, action domain.AuditAction) []entry {
	var matching []entry
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Action == string(action) {
			matching = append(matching, entries[i])
		}
	}
	return matching
}

func TestEveryActionWorthRememberingLeavesAnEntry(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	w := newApprovalWorld(ctx, t)
	environments := fmt.Sprintf("/projects/%s/environments", w.project)
	journal := fmt.Sprintf("/projects/%s/audit", w.project)

	// 1. A target declared, proved and approved.
	var declared struct {
		ID string `json:"id"`
	}
	if status := w.as(t, domain.RoleMaintainer, "POST", environments,
		`{"name":"staging","base_url":"https://shop.example.test","max_vus":5,"max_duration_s":60}`,
		&declared); status != http.StatusCreated {
		t.Fatalf("declaring a target got %d", status)
	}
	environment := environments + "/" + declared.ID

	var challenge struct {
		Token string `json:"token"`
		URL   string `json:"url"`
	}
	if status := w.as(t, domain.RoleMaintainer, "GET", environment+"/verification", "", &challenge); status != http.StatusOK {
		t.Fatalf("reading the challenge got %d", status)
	}

	// Verifying before the file is served: a refusal, and the journal keeps it.
	if status := w.as(t, domain.RoleMaintainer, "POST", environment+"/verify", "", nil); status != http.StatusUnprocessableEntity {
		t.Fatalf("verifying a target that serves nothing got %d", status)
	}

	w.targetHost.files[challenge.URL] = challenge.Token + "\n"
	if status := w.as(t, domain.RoleMaintainer, "POST", environment+"/verify", "", nil); status != http.StatusOK {
		t.Fatalf("verifying a target that serves its token got %d", status)
	}
	if status := w.as(t, domain.RoleAdmin, "POST", environment+"/approve", "", nil); status != http.StatusOK {
		t.Fatalf("approving got %d", status)
	}

	// 2. A run queued, and a run refused: the second target was never proved.
	var queued struct {
		ID string `json:"id"`
	}
	if status := w.as(t, domain.RoleRunner, "POST", "/runs", fmt.Sprintf(
		`{"scenario_id":%q,"environment_id":%q,"profile":"smoke"}`, w.scenario, declared.ID),
		&queued); status != http.StatusAccepted {
		t.Fatalf("queueing a run got %d", status)
	}

	var unverified struct {
		ID string `json:"id"`
	}
	if status := w.as(t, domain.RoleMaintainer, "POST", environments,
		`{"name":"nobody-proved-this","base_url":"https://not-ours.example.test","max_vus":5,"max_duration_s":60}`,
		&unverified); status != http.StatusCreated {
		t.Fatalf("declaring the second target got %d", status)
	}
	if status := w.as(t, domain.RoleRunner, "POST", "/runs", fmt.Sprintf(
		`{"scenario_id":%q,"environment_id":%q,"profile":"smoke"}`, w.scenario, unverified.ID),
		nil); status != http.StatusForbidden {
		t.Fatalf("a run against an unverified target got %d, want 403", status)
	}

	// 3. The run cancelled, and then everything stopped.
	if status := w.as(t, domain.RoleRunner, "DELETE", "/runs/"+queued.ID, "", nil); status != http.StatusOK {
		t.Fatalf("cancelling a queued run got %d", status)
	}
	if status := w.as(t, domain.RoleAdmin, "POST", "/runs/stop-all", "", nil); status != http.StatusOK {
		t.Fatalf("the emergency stop got %d", status)
	}

	// 4. A role changed.
	runner := w.member(domain.RoleRunner)
	if status := w.as(t, domain.RoleAdmin, "PUT",
		fmt.Sprintf("/projects/%s/members/%s", w.project, runner),
		`{"role":"viewer"}`, nil); status != http.StatusNoContent {
		t.Fatalf("changing a role got %d", status)
	}

	// The journal, read by the administrator of the project.
	var read struct {
		Entries []entry `json:"entries"`
	}
	if status := w.as(t, domain.RoleAdmin, "GET", journal, "", &read); status != http.StatusOK {
		t.Fatalf("reading the journal got %d", status)
	}

	// Every action of the ticket, with the four things an entry promises.
	expected := []struct {
		action  domain.AuditAction
		outcome domain.AuditOutcome
		target  string
		// orgWide says the action belongs to the organization rather than to
		// the project it is read from.
		orgWide bool
	}{
		{domain.ActionTargetVerified, domain.Refused, domain.TargetEnvironment, false},
		{domain.ActionTargetVerified, domain.Allowed, domain.TargetEnvironment, false},
		{domain.ActionTargetApproved, domain.Allowed, domain.TargetEnvironment, false},
		{domain.ActionRunQueued, domain.Allowed, domain.TargetRun, false},
		{domain.ActionRunRefused, domain.Refused, domain.TargetRun, false},
		{domain.ActionRunCancelled, domain.Allowed, domain.TargetRun, false},
		{domain.ActionRunsStopped, domain.Allowed, domain.TargetOrg, true},
		{domain.ActionRoleChanged, domain.Allowed, domain.TargetMembership, false},
	}

	for _, want := range expected {
		t.Run(string(want.action)+" "+string(want.outcome), func(t *testing.T) {
			var found *entry
			for _, candidate := range of(read.Entries, want.action) {
				if candidate.Outcome == string(want.outcome) {
					found = &candidate
					break
				}
			}
			if found == nil {
				t.Fatalf("the journal holds no %s that was %s", want.action, want.outcome)
			}

			switch {
			case found.ActorID == "":
				t.Error("the entry has no author")
			case found.TargetType != want.target:
				t.Errorf("the target type is %q, wanted %q", found.TargetType, want.target)
			case found.At.IsZero():
				t.Error("the entry has no date")
			case !want.orgWide && found.ProjectID != w.project.String():
				t.Errorf("the entry's project is %q", found.ProjectID)
			// An action on the organization is written once and read from
			// every project's journal rather than filed under one of them.
			case want.orgWide && found.ProjectID != "":
				t.Errorf("an organization-wide entry is filed under %q", found.ProjectID)
			case want.outcome == domain.Refused && found.Details["reason"] == "":
				t.Error("a refusal does not say why")
			}
		})
	}

	// Newest first, which is how it is read.
	for i := 1; i < len(read.Entries); i++ {
		if read.Entries[i].At.After(read.Entries[i-1].At) {
			t.Fatalf("entry %d is newer than the one before it", i)
		}
	}
}

// The journal is the record people are held to. Taking part in a project is not
// the same thing as reading what everybody in it did.
func TestOnlyAnAdministratorReadsTheJournal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newApprovalWorld(ctx, t)
	journal := fmt.Sprintf("/projects/%s/audit", w.project)

	for _, role := range []domain.Role{domain.RoleRunner, domain.RoleMaintainer} {
		if status := w.as(t, role, "GET", journal, "", nil); status != http.StatusForbidden {
			t.Errorf("a %s reading the journal got %d, want 403", role, status)
		}
	}

	if status := w.as(t, domain.RoleAdmin, "GET", journal, "", nil); status != http.StatusOK {
		t.Errorf("the administrator reading the journal got %d, want 200", status)
	}
}

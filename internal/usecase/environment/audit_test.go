package environment

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// What the journal holds about a target: who proved it owned, who failed to,
// and who decided load may be sent at it.

func TestAProvenTargetIsRecorded(t *testing.T) {
	f := newFixture(allowAll{})
	env := f.pending(t)

	if _, err := f.service.Verify(context.Background(), f.who, f.projectID, env.ID); err != nil {
		t.Fatalf("Verify: %v", err)
	}

	if len(f.journal.entries) != 1 {
		t.Fatalf("the journal holds %+v", f.journal.entries)
	}

	entry := f.journal.entries[0]
	switch {
	case entry.Action != domain.ActionTargetVerified || entry.Outcome != domain.Allowed:
		t.Errorf("the entry says %s / %s", entry.Action, entry.Outcome)
	case entry.ActorID == nil || *entry.ActorID != f.who.UserID:
		t.Errorf("the author is %v", entry.ActorID)
	case entry.TargetType != domain.TargetEnvironment || entry.TargetID == nil || *entry.TargetID != env.ID:
		t.Errorf("the target is %s %v", entry.TargetType, entry.TargetID)
	case entry.ProjectID == nil || *entry.ProjectID != f.projectID:
		t.Errorf("the project is %v", entry.ProjectID)
	case entry.At.IsZero():
		t.Error("the entry has no date")
	case entry.Details["host"] != "staging.example.test":
		t.Errorf("the details are %v", entry.Details)
	}
}

// A failed proof is worth as much to the journal as a successful one: somebody
// repeatedly failing to prove they own a host is the shape of an attempt to
// point Ballast at somebody else's server.
func TestATargetThatDidNotServeItsChallengeIsRecorded(t *testing.T) {
	f := newFixture(allowAll{})
	f.proof.err = errors.New("the challenge file was not served")
	env := f.pending(t)

	if _, err := f.service.Verify(context.Background(), f.who, f.projectID, env.ID); err == nil {
		t.Fatal("the verification went through")
	}

	if len(f.journal.entries) != 1 {
		t.Fatalf("the journal holds %+v", f.journal.entries)
	}

	entry := f.journal.entries[0]
	switch {
	case entry.Action != domain.ActionTargetVerified || entry.Outcome != domain.Refused:
		t.Errorf("the entry says %s / %s", entry.Action, entry.Outcome)
	case entry.Details["reason"] == "":
		t.Error("the entry does not say why")
	case entry.Details["host"] != "staging.example.test":
		t.Errorf("the details are %v", entry.Details)
	}
}

// The journal is read by more people than it is written by, so what goes in it
// is the host and never the proof URL — the URL carries the challenge token,
// which is what a target has to serve to prove it is somebody's.
func TestWhatIsRecordedAboutAVerificationNeverCarriesTheChallenge(t *testing.T) {
	f := newFixture(allowAll{})
	env := f.pending(t)

	if _, err := f.service.Verify(context.Background(), f.who, f.projectID, env.ID); err != nil {
		t.Fatalf("Verify: %v", err)
	}

	for key, value := range f.journal.entries[0].Details {
		if strings.Contains(value, "challenge-token") {
			t.Fatalf("the entry's %q carries the challenge: %q", key, value)
		}
	}
}

// The approval travels into the transaction that approves, so what a test reads
// is what that transaction was handed. An approval with no record of who gave
// it is the one thing this journal must never allow.
func TestAnApprovalCarriesItsEntryIntoTheTransaction(t *testing.T) {
	f := newFixture(allowAll{})
	env := f.inState(t, domain.VerificationProofOK)

	if _, err := f.service.Approve(context.Background(), f.who, f.projectID, env.ID); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	// Not written beside the approval: nothing reached the journal port.
	if len(f.journal.entries) != 0 {
		t.Fatalf("the approval was recorded separately: %+v", f.journal.entries)
	}

	entry := f.environments.approvalEntry
	switch {
	case entry.Action != domain.ActionTargetApproved || entry.Outcome != domain.Allowed:
		t.Errorf("the entry says %s / %s", entry.Action, entry.Outcome)
	case entry.ActorID == nil || *entry.ActorID != f.who.UserID:
		t.Errorf("the author is %v", entry.ActorID)
	case entry.TargetID == nil || *entry.TargetID != env.ID:
		t.Errorf("the target is %v", entry.TargetID)
	case entry.ProjectID == nil || *entry.ProjectID != f.projectID:
		t.Errorf("the project is %v", entry.ProjectID)
	case entry.At.IsZero():
		t.Error("the entry has no date")
	// The host is the point of the entry: it is what an abuse report names.
	case entry.Details["host"] != "staging.example.test":
		t.Errorf("the details are %v", entry.Details)
	}
}

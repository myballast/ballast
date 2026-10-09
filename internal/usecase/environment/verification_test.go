// Written before the implementation. What this file pins down is the division
// the ticket insists on: proving a target and approving it are two separate
// steps, held by two different permissions, and the second is the one that lets
// load through.
package environment

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// fakeProof stands in for the verifier. What it decides is tested in
// adapter/verifier; what matters here is that the service asks, with which URL,
// and what it does with the answer.
type fakeProof struct {
	err      error
	url      string
	expected string
	calls    int
}

func (f *fakeProof) ReadProof(_ context.Context, proofURL, expected string) error {
	f.calls++
	f.url, f.expected = proofURL, expected
	return f.err
}

// pending is a declared target, in the state Create leaves one in.
//
// Put in place behind the service rather than through Create: these tests are
// about what the verification refuses, and several of them hand the caller a
// permission that could not have created an environment in the first place.
func (f fixture) pending(t *testing.T) domain.Environment {
	t.Helper()

	return f.inState(t, domain.VerificationPending)
}

func (f fixture) inState(t *testing.T, status domain.VerificationStatus) domain.Environment {
	t.Helper()

	env := domain.Environment{
		ID:                 uuid.New(),
		ProjectID:          f.projectID,
		Name:               "staging",
		BaseURL:            "https://staging.example.test",
		VerificationStatus: status,
		VerificationToken:  "a-challenge-token",
		MaxVUs:             50,
		MaxDurationS:       600,
	}
	f.environments.byID[env.ID] = env
	return env
}

func TestAProvenTargetIsNotYetAllowedToTakeLoad(t *testing.T) {
	f := newFixture(allowAll{})
	env := f.pending(t)

	proven, err := f.service.Verify(context.Background(), f.who, f.projectID, env.ID)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	if proven.VerificationStatus != domain.VerificationProofOK {
		t.Fatalf("a proven target is %q, want proof_ok", proven.VerificationStatus)
	}
	if proven.IsVerified() {
		t.Fatal("serving a file was enough to make a target verified")
	}
	if f.environments.approved != 0 {
		t.Fatal("the repository was asked to approve a target nobody approved")
	}
}

func TestTheChallengeIsFetchedAtTheURLTheTargetWasTold(t *testing.T) {
	f := newFixture(allowAll{})
	env := f.pending(t)

	if _, err := f.service.Verify(context.Background(), f.who, f.projectID, env.ID); err != nil {
		t.Fatalf("Verify: %v", err)
	}

	wanted, err := env.ProofURL()
	if err != nil {
		t.Fatalf("the test's own environment has no challenge URL: %v", err)
	}

	if f.proof.calls != 1 {
		t.Fatalf("the proof was fetched %d times", f.proof.calls)
	}
	if f.proof.url != wanted {
		t.Errorf("fetched %s, want %s", f.proof.url, wanted)
	}
	if f.proof.expected != env.VerificationToken {
		t.Errorf("checked against %q, want the environment's own token", f.proof.expected)
	}
}

// The first acceptance criterion of the ticket, at this layer: no file, no
// proof, and the row records that an attempt was made and failed.
func TestAMissingProofLeavesTheTargetUnverified(t *testing.T) {
	f := newFixture(allowAll{})
	f.proof.err = errors.New("the challenge file was not served")
	env := f.pending(t)

	_, err := f.service.Verify(context.Background(), f.who, f.projectID, env.ID)
	if !errors.Is(err, domain.ErrVerificationFailed) {
		t.Fatalf("got %v, want ErrVerificationFailed", err)
	}

	stored := f.environments.byID[env.ID]
	if stored.VerificationStatus != domain.VerificationFailed {
		t.Fatalf("after a failed attempt the target is %q", stored.VerificationStatus)
	}
	if stored.IsVerified() {
		t.Fatal("a failed attempt left the target verified")
	}
}

// The reason has to travel: "it did not work" is not something anybody can act
// on, and the four reasons are fixed four different ways.
func TestAFailedAttemptSaysWhy(t *testing.T) {
	f := newFixture(allowAll{})
	f.proof.err = errors.New("the target does not resolve to a publicly routable address")
	env := f.pending(t)

	_, err := f.service.Verify(context.Background(), f.who, f.projectID, env.ID)
	if err == nil {
		t.Fatal("a refused address passed")
	}
	if !strings.Contains(err.Error(), "publicly routable") {
		t.Fatalf("the refusal lost its reason: %v", err)
	}
}

func TestApprovingAProvenTargetLetsLoadThrough(t *testing.T) {
	f := newFixture(allowAll{})
	env := f.inState(t, domain.VerificationProofOK)

	approved, err := f.service.Approve(context.Background(), f.who, f.projectID, env.ID)
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}

	if !approved.IsVerified() {
		t.Fatalf("an approved target is %q", approved.VerificationStatus)
	}
	if f.environments.approvedBy != f.who.UserID {
		t.Error("the approval does not record who gave it")
	}
	if f.environments.allowedHost != "staging.example.test" {
		t.Errorf("the project allows %q after the approval", f.environments.allowedHost)
	}
}

// The second acceptance criterion: a valid proof with nobody's approval is not
// a target load may be sent to. The run service is what refuses, and it reads
// IsVerified, so this holds it at the source.
func TestAProofWithoutAnApprovalIsNotEnough(t *testing.T) {
	f := newFixture(allowAll{})
	env := f.pending(t)

	proven, err := f.service.Verify(context.Background(), f.who, f.projectID, env.ID)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if proven.IsVerified() {
		t.Fatal("the proof alone made the target verified")
	}

	// And nothing was added to the project's allowlist either.
	if f.environments.allowedHost != "" {
		t.Errorf("a proven target already allows %q", f.environments.allowedHost)
	}
}

func TestApprovingWithoutAProofIsRefused(t *testing.T) {
	f := newFixture(allowAll{})

	for _, status := range []domain.VerificationStatus{
		domain.VerificationPending,
		domain.VerificationFailed,
	} {
		env := f.inState(t, status)

		if err := approveErr(f, env.ID); !errors.Is(err, domain.ErrNotProven) {
			t.Errorf("approving a %q target answered %v, want ErrNotProven", status, err)
		}
		if f.environments.approved != 0 {
			t.Fatalf("a %q target was approved", status)
		}
	}
}

// Managing environments and approving a target are deliberately different
// rights: a maintainer declares targets, an admin is the one who says load may
// be sent at one. Without this, whoever can create an environment can approve
// their own, and the human step is a formality.
func TestApprovingTakesMoreThanManagingEnvironments(t *testing.T) {
	f := newFixture(grantOnly{perm: domain.PermManageEnvironments})
	env := f.inState(t, domain.VerificationProofOK)

	// The same caller may run the verification.
	if _, err := f.service.Verify(context.Background(), f.who, f.projectID, env.ID); err != nil {
		t.Fatalf("a maintainer could not verify a target: %v", err)
	}

	if err := approveErr(f, env.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("a maintainer approved a target: %v", err)
	}
	if f.environments.approved != 0 {
		t.Fatal("the target was approved anyway")
	}
}

func TestOnlyTheApproveTargetPermissionApproves(t *testing.T) {
	f := newFixture(grantOnly{perm: domain.PermApproveTarget})
	env := f.inState(t, domain.VerificationProofOK)

	if err := approveErr(f, env.ID); err != nil {
		t.Fatalf("the permission to approve did not approve: %v", err)
	}
}

func TestVerifyingTakesThePermissionToManageEnvironments(t *testing.T) {
	f := newFixture(grantOnly{perm: domain.PermViewProject})
	env := f.pending(t)

	_, err := f.service.Verify(context.Background(), f.who, f.projectID, env.ID)
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("a viewer ran a verification: %v", err)
	}
	if f.proof.calls != 0 {
		t.Fatal("a request Ballast refused still went out to the target")
	}
}

// The challenge is what somebody has to put on their server, so whoever manages
// the environment may read it -- and nobody else. A plain reader of the project
// has no file to create.
func TestTheChallengeIsNotForEveryReader(t *testing.T) {
	f := newFixture(grantOnly{perm: domain.PermViewProject})
	env := f.pending(t)

	if _, err := f.service.Challenge(context.Background(), f.who, f.projectID, env.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("a viewer read the challenge token: %v", err)
	}
}

func TestTheChallengeTellsWhatToServe(t *testing.T) {
	f := newFixture(allowAll{})
	env := f.pending(t)

	read, err := f.service.Challenge(context.Background(), f.who, f.projectID, env.ID)
	if err != nil {
		t.Fatalf("Challenge: %v", err)
	}

	if read.VerificationToken != env.VerificationToken {
		t.Errorf("the token read back is %q", read.VerificationToken)
	}
	if _, err := read.ProofURL(); err != nil {
		t.Errorf("the challenge has no URL to serve: %v", err)
	}
}

// Row-level security puts both rows in the caller's organization and says
// nothing about them being in the same project. A target of another project,
// reached through one the caller administers, is not theirs to verify or approve.
func TestATargetOfAnotherProjectIsNotThere(t *testing.T) {
	f := newFixture(allowAll{})
	env := f.inState(t, domain.VerificationProofOK)
	elsewhere := uuid.New()

	ctx := context.Background()

	if _, err := f.service.Verify(ctx, f.who, elsewhere, env.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Verify answered %v, want not found", err)
	}
	if _, err := f.service.Approve(ctx, f.who, elsewhere, env.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Approve answered %v, want not found", err)
	}
	if _, err := f.service.Challenge(ctx, f.who, elsewhere, env.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Challenge answered %v, want not found", err)
	}
	if f.proof.calls != 0 {
		t.Fatal("a request about another project's target went out")
	}
}

// Running the verification on an approved target would move it back to proof_ok
// and quietly take its approval away.
func TestAnApprovedTargetIsNotVerifiedAgain(t *testing.T) {
	f := newFixture(allowAll{})
	env := f.inState(t, domain.VerificationVerified)

	_, err := f.service.Verify(context.Background(), f.who, f.projectID, env.ID)
	if !errors.Is(err, domain.ErrAlreadyVerified) {
		t.Fatalf("got %v, want ErrAlreadyVerified", err)
	}

	if stored := f.environments.byID[env.ID]; !stored.IsVerified() {
		t.Fatal("the attempt took the approval away")
	}
	if f.proof.calls != 0 {
		t.Fatal("the target was fetched for nothing")
	}
}

// Every attempt and every approval is logged: this is the control an abuse
// report is answered with, and it has to be possible to say who approved what
// and when it was tried.
func TestEveryAttemptIsLogged(t *testing.T) {
	f := newFixture(allowAll{})
	env := f.inState(t, domain.VerificationProofOK)
	ctx := context.Background()

	if _, err := f.service.Verify(ctx, f.who, f.projectID, env.ID); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if _, err := f.service.Approve(ctx, f.who, f.projectID, env.ID); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	logged := f.logged.String()
	for _, want := range []string{env.ID.String(), f.who.UserID.String()} {
		if !strings.Contains(logged, want) {
			t.Errorf("the log does not name %s:\n%s", want, logged)
		}
	}
	// The token is not a credential, but it has no business in a log either.
	if strings.Contains(logged, "a-challenge-token") {
		t.Errorf("the log carries the challenge token:\n%s", logged)
	}
}

func approveErr(f fixture, id uuid.UUID) error {
	_, err := f.service.Approve(context.Background(), f.who, f.projectID, id)
	return err
}

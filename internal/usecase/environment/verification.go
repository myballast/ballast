package environment

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// Challenge returns what a target has to serve to prove somebody owns it.
//
// It takes the permission to manage environments rather than the one to view the
// project: whoever has to put the file on the server needs this, and nobody else
// has anything to do with it.
func (s *Service) Challenge(
	ctx context.Context, who domain.Identity, projectID, id uuid.UUID,
) (domain.Environment, error) {
	return s.read(ctx, who, domain.PermManageEnvironments, projectID, id)
}

// Verify fetches the challenge from the target and records whether it was there.
//
// A successful proof leaves the environment in proof_ok, which is **not** a
// target load may be sent to. Approving it is a separate decision, taken by a
// person, under a separate permission. The two steps exist because automatic
// approval falls the day somebody finds a way to serve a file on a domain that
// is not theirs.
func (s *Service) Verify(
	ctx context.Context, who domain.Identity, projectID, id uuid.UUID,
) (domain.Environment, error) {
	found, err := s.read(ctx, who, domain.PermManageEnvironments, projectID, id)
	if err != nil {
		return domain.Environment{}, err
	}
	if err := found.RequireProvable(); err != nil {
		return domain.Environment{}, err
	}

	proofURL, err := found.ProofURL()
	if err != nil {
		return domain.Environment{}, err
	}

	// The one outbound request Ballast makes to an address somebody else chose.
	// What keeps it safe is behind the port.
	if readErr := s.proof.ReadProof(ctx, proofURL, found.VerificationToken); readErr != nil {
		// The host, not the URL: the URL carries the token.
		s.log.WarnContext(ctx, "target verification failed",
			"org_id", who.OrgID, "user_id", who.UserID,
			"environment_id", id, "host", hostOf(found),
			"reason", readErr.Error())

		failed, err := s.environments.RecordProofFailed(ctx, who.OrgID, id, s.clock.Now())
		if err != nil {
			return domain.Environment{}, err
		}

		// A failed verification is worth as much to the journal as a successful
		// one: somebody repeatedly failing to prove they own a host is the
		// shape of an attempt to point Ballast at a server that is not theirs.
		// The reason is this code's own words — an error from deeper down can
		// name an address or an identifier that has no business here.
		s.record(ctx, domain.Refuse(
			who.OrgID, &who.UserID, domain.ActionTargetVerified, domain.TargetEnvironment,
			"the target did not serve its challenge", s.clock.Now(),
		).InProject(found.ProjectID).On(id).With("host", hostOf(found)))

		// Wrapped with a description rather than the bare error: callers match
		// on the sentinel, and the person who declared the environment needs to
		// be told where Ballast looked, what answered and how it differed,
		// since the reasons are fixed in different places.
		return failed, fmt.Errorf("%w: %w", domain.ErrVerificationFailed,
			domain.DescribeProofFailure(proofURL, readErr))
	}

	proven, err := s.environments.RecordProofOK(ctx, who.OrgID, id, s.clock.Now())
	if err != nil {
		return domain.Environment{}, err
	}

	s.log.InfoContext(ctx, "target proof accepted, awaiting approval",
		"org_id", who.OrgID, "user_id", who.UserID,
		"environment_id", id, "host", hostOf(found))

	// The host, never the URL: the URL carries the challenge token, and a
	// journal is read by more people than it is written by.
	s.record(ctx, domain.Allow(
		who.OrgID, &who.UserID, domain.ActionTargetVerified, domain.TargetEnvironment, s.clock.Now(),
	).InProject(found.ProjectID).On(id).With("host", hostOf(found)))

	return proven, nil
}

// Approve is the human decision that lets load reach a target.
//
// It is the last gate in front of traffic leaving Ballast's servers, which is
// why it holds its own permission: a maintainer declares targets and runs their
// verification, an administrator is the one who says load may be sent at one.
// Were it the same right, whoever creates an environment approves their own and
// the whole arrangement is decorative.
//
// Approving also adds the target's host to the project's allowlist, in the same
// transaction: a target nothing may reach is not approved, and an allowlist that
// disagrees with the approvals is worse than either.
func (s *Service) Approve(
	ctx context.Context, who domain.Identity, projectID, id uuid.UUID,
) (domain.Environment, error) {
	found, err := s.read(ctx, who, domain.PermApproveTarget, projectID, id)
	if err != nil {
		return domain.Environment{}, err
	}
	if err := found.RequireApprovable(); err != nil {
		return domain.Environment{}, err
	}

	host, err := found.Host()
	if err != nil {
		return domain.Environment{}, err
	}

	// The entry travels into the transaction that approves, rather than being
	// written after it. Approving is what lets traffic leave Ballast's servers
	// at somebody's host; if the journal entry were a separate write, a process
	// dying in between would leave load authorised with nobody's name on it —
	// which is the one question this journal exists to answer.
	at := s.clock.Now()
	entry := domain.Allow(
		who.OrgID, &who.UserID, domain.ActionTargetApproved, domain.TargetEnvironment, at,
	).InProject(found.ProjectID).On(id).With("host", host)

	approved, err := s.environments.Approve(ctx, who.OrgID, id, who, at, host, entry)
	if err != nil {
		return domain.Environment{}, err
	}

	// Worth a line at this level: this is the record an abuse report is answered
	// with — who allowed Ballast to send load at this host, and when.
	s.log.WarnContext(ctx, "target approved for load",
		"org_id", who.OrgID, "user_id", who.UserID,
		"environment_id", id, "project_id", found.ProjectID, "host", host)

	return approved, nil
}

// hostOf is the host for a log line, and never an error: a line that cannot be
// written because a URL is malformed is worse than a line with a gap in it.
func hostOf(environment domain.Environment) string {
	host, err := environment.Host()
	if err != nil {
		return "unreadable"
	}
	return host
}

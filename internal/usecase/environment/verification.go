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

		failed, err := s.environments.RecordProofFailed(ctx, who.OrgID, id)
		if err != nil {
			return domain.Environment{}, err
		}

		// Both wrapped: callers match on the sentinel, and the person who
		// declared the environment needs the reason, since the four of them are
		// fixed four different ways.
		return failed, fmt.Errorf("%w: %w", domain.ErrVerificationFailed, readErr)
	}

	proven, err := s.environments.RecordProofOK(ctx, who.OrgID, id)
	if err != nil {
		return domain.Environment{}, err
	}

	s.log.InfoContext(ctx, "target proof accepted, awaiting approval",
		"org_id", who.OrgID, "user_id", who.UserID,
		"environment_id", id, "host", hostOf(found))

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

	approved, err := s.environments.Approve(ctx, who.OrgID, id, who, s.clock.Now(), host)
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

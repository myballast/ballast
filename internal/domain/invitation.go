package domain

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
)

// InvitationTTL is how long an invitation link works. A link is a bearer
// credential that travels through chat and mail, so it is short-lived; a person
// who missed it is invited again.
const InvitationTTL = 7 * 24 * time.Hour

// ErrInvalidInvitation reports an invitation Ballast will not create: an address
// that is not one, or a role that does not exist. A mistake in the request, not
// a refusal.
var ErrInvalidInvitation = errors.New("invalid invitation")

// ErrInvitationUnusable reports a link that no longer works: used, revoked or
// expired. Wrapped with which, because the reasons are answered differently — an
// expired link is asked for again, a revoked one was withdrawn on purpose.
var ErrInvitationUnusable = errors.New("the invitation can no longer be used")

// ErrWrongInvitee reports an invitation followed by somebody whose verified
// address is not the one it was written for. The link is a credential, but a
// credential for one person: a link forwarded on does not make its holder a
// member.
var ErrWrongInvitee = errors.New("the invitation is for another address")

// ErrAlreadyMember reports an invitation to somebody who already belongs to the
// project.
var ErrAlreadyMember = errors.New("already a member of the project")

// ErrRoleAboveOwn reports an attempt to grant a role higher than the caller's
// own, or to change the role of somebody who holds one. Whoever can manage
// members can manage only up to themselves.
var ErrRoleAboveOwn = errors.New("that role is above your own")

// InvitationState is what became of an invitation.
type InvitationState string

const (
	// InvitationPending can still be followed.
	InvitationPending InvitationState = "pending"
	// InvitationAccepted was followed, once.
	InvitationAccepted InvitationState = "accepted"
	// InvitationRevoked was withdrawn by an administrator.
	InvitationRevoked InvitationState = "revoked"
	// InvitationExpired went unused past its date.
	InvitationExpired InvitationState = "expired"
)

// Invitation is a promise of a role in a project, made to an address.
//
// It holds no token: only the hash of one is ever stored, and the token itself is
// shown once, to the person who created the invitation.
type Invitation struct {
	ID         uuid.UUID
	OrgID      uuid.UUID
	ProjectID  uuid.UUID
	Email      string
	Role       Role
	InvitedBy  *uuid.UUID
	ExpiresAt  time.Time
	AcceptedAt *time.Time
	RevokedAt  *time.Time
	CreatedAt  time.Time
}

// State is what became of the invitation at the moment asked. Used and revoked
// outrank expired: an invitation that was used and has since passed its date was
// still used.
func (i Invitation) State(now time.Time) InvitationState {
	switch {
	case i.AcceptedAt != nil:
		return InvitationAccepted
	case i.RevokedAt != nil:
		return InvitationRevoked
	case !now.Before(i.ExpiresAt):
		return InvitationExpired
	}
	return InvitationPending
}

// ValidateInvitation checks what an inviter wrote and returns the address in the
// form it is kept: lower case, with no display name around it.
func ValidateInvitation(email string, role Role) (string, error) {
	address, err := mail.ParseAddress(strings.TrimSpace(email))
	if err != nil || address.Name != "" || strings.ContainsAny(address.Address, " ,;<>") {
		return "", fmt.Errorf("%w: %q is not an email address", ErrInvalidInvitation, email)
	}
	if !role.IsValid() {
		return "", fmt.Errorf("%w: %q is not a role", ErrInvalidInvitation, role)
	}
	return strings.ToLower(address.Address), nil
}

// SameAddress compares two addresses the way the invitation kept them.
func SameAddress(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// Rank orders the roles, least able first. Zero for a role Ballast does not know,
// so that an unknown role outranks nothing and is outranked by every real one.
func (r Role) Rank() int {
	for i, role := range Roles() {
		if role == r {
			return i + 1
		}
	}
	return 0
}

// CanGrant reports whether somebody holding actor may give target, or change the
// role of somebody who holds it: never above their own.
func CanGrant(actor, target Role) bool {
	return actor.IsValid() && target.IsValid() && target.Rank() <= actor.Rank()
}

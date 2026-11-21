package domain

import (
	"errors"

	"github.com/google/uuid"
)

// ErrUnauthenticated reports a request that carries no valid session.
var ErrUnauthenticated = errors.New("unauthenticated")

// ErrIncompleteIdentity reports an identity provider that did not vouch for
// everything Ballast needs to know about a person: a stable subject and a
// verified email address.
var ErrIncompleteIdentity = errors.New("incomplete identity")

// Identity is who is making a request, and on behalf of which organization.
//
// It carries no permission. Permissions are resolved against the database on
// every request, so that a revoked role takes effect immediately rather than
// when a session happens to expire.
//
// It is either a person or an API token, never both. A token has no person: its
// UserID is empty, and TokenID says which token is asking.
type Identity struct {
	UserID  uuid.UUID
	OrgID   uuid.UUID
	TokenID uuid.UUID
}

// IsToken reports whether the caller is an API token and not a person.
func (i Identity) IsToken() bool {
	return i.TokenID != uuid.Nil
}

// ExternalIdentity is a person as an identity provider describes them.
type ExternalIdentity struct {
	// Subject is the provider's stable identifier for the person, prefixed with
	// the provider's name: "github|583231". Two providers may hand out the same
	// raw identifier to two different people; the prefix keeps them apart.
	Subject string
	// Email is an address the provider has verified.
	Email string
	Name  string
}

// Validate refuses an identity Ballast cannot create an account from.
func (e ExternalIdentity) Validate() error {
	switch {
	case e.Subject == "":
		return errors.Join(ErrIncompleteIdentity, errors.New("no subject"))
	case e.Email == "":
		return errors.Join(ErrIncompleteIdentity, errors.New("no verified email"))
	}
	return nil
}

// Role is what a member may do within a project. Stored as text in
// membership.role; the permission mask is derived from it in code.
type Role string

// The four roles, from least to most able. They are cumulative: each grants
// everything the one before it does, which is what the use case diagram's
// "inherits from" arrows mean. The values are stored in membership.role and
// constrained by migration 001.
const (
	RoleViewer     Role = "viewer"
	RoleRunner     Role = "runner"
	RoleMaintainer Role = "maintainer"
	// RoleAdmin is also the role of whoever created an organization.
	RoleAdmin Role = "admin"
)

// Roles returns the four roles, least able first.
func Roles() []Role {
	return []Role{RoleViewer, RoleRunner, RoleMaintainer, RoleAdmin}
}

// IsValid reports whether r is one of the four.
func (r Role) IsValid() bool {
	_, ok := roleMasks[r]
	return ok
}

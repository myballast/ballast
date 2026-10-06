package domain

import (
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
)

// ErrInvalidProject reports a project that cannot be created or renamed as
// asked.
var ErrInvalidProject = errors.New("invalid project")

// ErrProjectNotEmpty reports an attempt to delete a project that still holds
// runs. Its foreign keys cascade, so deleting it would take the history with
// it, and an accident there is not recoverable.
var ErrProjectNotEmpty = errors.New("the project still holds runs")

// ErrAlreadyExists reports a name or slug another row already uses.
var ErrAlreadyExists = errors.New("already exists")

// Project is an isolated space: its own scenarios, environments, runs and
// members.
type Project struct {
	ID        uuid.UUID
	OrgID     uuid.UUID
	Name      string
	Slug      string
	CreatedAt time.Time
}

// Membership is what one person may do in one project.
type Membership struct {
	OrgID     uuid.UUID
	UserID    uuid.UUID
	ProjectID uuid.UUID
	Role      Role
	// Override mirrors membership.permissions: a mask stored against this one
	// membership, which replaces the role's. Nothing in Ballast writes it.
	Override  *int64
	CreatedAt time.Time
}

// Permissions is what this membership grants, and whether its role is known.
func (m Membership) Permissions() (PermissionSet, bool) {
	return PermissionsFor(m.Role, m.Override)
}

const (
	maxProjectNameLength = 80
	maxProjectSlugLength = 40
)

// A slug appears in URLs and in object storage keys, so it is kept to what is
// unambiguous there: lowercase, digits, and single hyphens between them.
var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ValidateProject refuses a name or slug Ballast will not store.
func ValidateProject(name, slug string) error {
	switch {
	case name == "":
		return fmt.Errorf("%w: the name is empty", ErrInvalidProject)
	case len(name) > maxProjectNameLength:
		return fmt.Errorf("%w: the name is longer than %d characters", ErrInvalidProject, maxProjectNameLength)
	case slug == "":
		return fmt.Errorf("%w: the slug is empty", ErrInvalidProject)
	case len(slug) > maxProjectSlugLength:
		return fmt.Errorf("%w: the slug is longer than %d characters", ErrInvalidProject, maxProjectSlugLength)
	case !slugPattern.MatchString(slug):
		return fmt.Errorf("%w: the slug may hold lowercase letters, digits and single hyphens", ErrInvalidProject)
	}
	return nil
}

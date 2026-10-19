package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

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
	ID    uuid.UUID
	OrgID uuid.UUID
	Name  string
	Slug  string
	// Details are what the projects page shows beside the name. All optional.
	Details   ProjectDetails
	CreatedAt time.Time
}

// ProjectDetails says where a project runs, who answers for it, and what it is
// made of. Every field may be empty, and the screens leave an empty one out
// instead of showing a blank.
type ProjectDetails struct {
	// Infra is a short free text: k8s, railway, vm.
	Infra string
	// Region is where it runs: eu-west-1.
	Region string
	// Owner is a person or a team, as text. It is not a user: nothing is
	// granted by it.
	Owner string
	// Stack is short labels for what it is made of: go, pg, k6.
	Stack []string
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

const (
	maxInfraLength  = 40
	maxRegionLength = 40
	maxOwnerLength  = 80
	maxStackTags    = 12
	maxStackTagLen  = 16
)

// A stack label is what fits on a small chip: lowercase letters and digits, and
// the few punctuation marks real names carry (node.js, c++, ci-cd).
var stackTagPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._+-]*$`)

// Clean validates what a person wrote and returns it in the form it is kept:
// trimmed, and with the stack in lowercase, each label once, in the order given.
func (d ProjectDetails) Clean() (ProjectDetails, error) {
	clean := ProjectDetails{
		Infra:  strings.TrimSpace(d.Infra),
		Region: strings.TrimSpace(d.Region),
		Owner:  strings.TrimSpace(d.Owner),
		Stack:  []string{},
	}

	for field, limit := range map[string]struct {
		value string
		max   int
	}{
		"infra":  {clean.Infra, maxInfraLength},
		"region": {clean.Region, maxRegionLength},
		"owner":  {clean.Owner, maxOwnerLength},
	} {
		if utf8.RuneCountInString(limit.value) > limit.max {
			return ProjectDetails{}, fmt.Errorf("%w: %s is longer than %d characters", ErrInvalidProject, field, limit.max)
		}
		if strings.ContainsFunc(limit.value, unicode.IsControl) {
			return ProjectDetails{}, fmt.Errorf("%w: %s holds a control character", ErrInvalidProject, field)
		}
	}

	seen := map[string]bool{}
	for _, tag := range d.Stack {
		tag = strings.ToLower(strings.TrimSpace(tag))
		switch {
		case tag == "":
			continue
		case len(tag) > maxStackTagLen:
			return ProjectDetails{}, fmt.Errorf("%w: the stack label %q is longer than %d characters", ErrInvalidProject, tag, maxStackTagLen)
		case !stackTagPattern.MatchString(tag):
			return ProjectDetails{}, fmt.Errorf("%w: the stack label %q may hold lowercase letters, digits and . _ + -", ErrInvalidProject, tag)
		case seen[tag]:
			continue
		}
		seen[tag] = true
		clean.Stack = append(clean.Stack, tag)
	}
	if len(clean.Stack) > maxStackTags {
		return ProjectDetails{}, fmt.Errorf("%w: the stack holds more than %d labels", ErrInvalidProject, maxStackTags)
	}

	return clean, nil
}

// Place is where a project runs as a card says it: "k8s · eu-west-1", or either
// half when only one is known, or nothing.
func (d ProjectDetails) Place() string {
	switch {
	case d.Infra != "" && d.Region != "":
		return d.Infra + " · " + d.Region
	case d.Infra != "":
		return d.Infra
	}
	return d.Region
}

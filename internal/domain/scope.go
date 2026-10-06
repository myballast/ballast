package domain

import "github.com/google/uuid"

// RequireSameProject refuses identifiers that do not belong together.
//
// A permission mask answers what somebody may do; it never answers what to.
// Row-level security keeps one organization's rows away from another's, and has
// nothing to say about two projects of the same organization — so when several
// identifiers arrive in one request, that they agree is something only this
// check establishes.
//
// A nil identifier is refused rather than treated as "any project". It stands
// for a project nobody named, and letting it through would be a row with no
// project passing a check that looks like it succeeded. Being handed nothing to
// compare is refused for the same reason: it is a caller that lost its
// arguments, not an agreement.
func RequireSameProject(projectIDs ...uuid.UUID) error {
	if len(projectIDs) == 0 {
		return ErrCrossProject
	}

	first := projectIDs[0]
	if first == uuid.Nil {
		return ErrCrossProject
	}

	for _, id := range projectIDs[1:] {
		if id != first {
			return ErrCrossProject
		}
	}

	return nil
}

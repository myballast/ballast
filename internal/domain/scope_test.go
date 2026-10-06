package domain

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

// A permission mask answers what somebody may do, never what to. Row-level
// security keeps one organization's rows away from another's and says nothing
// about two projects of the same organization, so identifiers that arrive
// together in one request have to be checked against each other.
func TestRequireSameProject(t *testing.T) {
	a, b := uuid.New(), uuid.New()

	cases := []struct {
		name    string
		ids     []uuid.UUID
		wantErr error
	}{
		{"one identifier", []uuid.UUID{a}, nil},
		{"two from the same project", []uuid.UUID{a, a}, nil},
		{"three from the same project", []uuid.UUID{a, a, a}, nil},

		{"two different projects", []uuid.UUID{a, b}, ErrCrossProject},
		{"one of three differs", []uuid.UUID{a, a, b}, ErrCrossProject},
		{"the first differs", []uuid.UUID{b, a, a}, ErrCrossProject},

		// A nil identifier is not "any project", it is a project nobody named.
		// Accepting it would let a row with no project slip through a check that
		// looks like it passed.
		{"nil alone", []uuid.UUID{uuid.Nil}, ErrCrossProject},
		{"nil among equals", []uuid.UUID{a, uuid.Nil}, ErrCrossProject},
		{"all nil", []uuid.UUID{uuid.Nil, uuid.Nil}, ErrCrossProject},

		// Nothing to compare is a caller that forgot its arguments, not an
		// agreement.
		{"nothing at all", nil, ErrCrossProject},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := RequireSameProject(c.ids...)

			if c.wantErr == nil && err != nil {
				t.Fatalf("identifiers that belong together were refused: %v", err)
			}
			if c.wantErr != nil && !errors.Is(err, c.wantErr) {
				t.Fatalf("got %v, want %v", err, c.wantErr)
			}
		})
	}
}

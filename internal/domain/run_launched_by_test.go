package domain

import (
	"testing"

	"github.com/google/uuid"
)

// The right of a token to cancel is read from the run: it may take down what it
// launched and nothing else.
func TestARunIsLaunchedByTheTokenItNames(t *testing.T) {
	token, another, person := uuid.New(), uuid.New(), uuid.New()

	cases := []struct {
		name string
		run  Run
		who  uuid.UUID
		want bool
	}{
		{"the token that launched it", Run{TriggeredByToken: &token}, token, true},
		{"another token", Run{TriggeredByToken: &another}, token, false},
		{"a run a person launched", Run{TriggeredBy: &person}, token, false},
		{"a run nobody is recorded for", Run{}, token, false},
		// The zero identifier must never match a run that names no token: both
		// sides empty is not "the same token".
		{"no token asking about a run of nobody", Run{}, uuid.Nil, false},
		{"no token asking about a run of a token", Run{TriggeredByToken: &token}, uuid.Nil, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.run.LaunchedByToken(c.who); got != c.want {
				t.Errorf("LaunchedByToken = %v, want %v", got, c.want)
			}
		})
	}
}

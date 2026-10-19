package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestDetailsAreKeptTrimmedAndTheStackInLowercaseOnce(t *testing.T) {
	t.Parallel()

	got, err := ProjectDetails{
		Infra: " k8s ", Region: "eu-west-1", Owner: " Payments team ",
		Stack: []string{"Go", " pg ", "go", "", "Node.js", "c++"},
	}.Clean()
	if err != nil {
		t.Fatalf("Clean: %v", err)
	}

	if got.Infra != "k8s" || got.Owner != "Payments team" {
		t.Errorf("got %+v", got)
	}
	if strings.Join(got.Stack, ",") != "go,pg,node.js,c++" {
		t.Errorf("stack %v", got.Stack)
	}
}

// Nothing said is nothing stored: an empty stack is an empty slice and not nil,
// so that it is written as an empty array and not as a null.
func TestNoDetailsIsValid(t *testing.T) {
	t.Parallel()

	got, err := ProjectDetails{}.Clean()
	if err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if got.Stack == nil || len(got.Stack) != 0 || got.Infra != "" || got.Place() != "" {
		t.Errorf("got %+v", got)
	}
}

func TestDetailsBallastWillNotStore(t *testing.T) {
	t.Parallel()

	cases := map[string]ProjectDetails{
		"a long infra":                 {Infra: strings.Repeat("x", 41)},
		"a long region":                {Region: strings.Repeat("x", 41)},
		"a long owner":                 {Owner: strings.Repeat("x", 81)},
		"a control character":          {Owner: "a\x00b"},
		"a newline":                    {Infra: "k8s\nrailway"},
		"a label with space":           {Stack: []string{"node js"}},
		"a label with a slash":         {Stack: []string{"a/b"}},
		"a label too long":             {Stack: []string{strings.Repeat("a", 17)}},
		"a label with accent":          {Stack: []string{"é"}},
		"a label starting with a mark": {Stack: []string{"-go"}},
		"thirteen labels":              {Stack: []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m"}},
	}
	for name, details := range cases {
		if _, err := details.Clean(); !errors.Is(err, ErrInvalidProject) {
			t.Errorf("%s: got %v", name, err)
		}
	}
}

// Twelve is the limit and not eleven; a repeat does not count twice.
func TestTheStackLimitCountsDistinctLabels(t *testing.T) {
	t.Parallel()

	twelve := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "a", "b"}
	if _, err := (ProjectDetails{Stack: twelve}).Clean(); err != nil {
		t.Errorf("twelve distinct labels with repeats: %v", err)
	}
}

func TestAPlaceIsSaidWithWhatIsKnown(t *testing.T) {
	t.Parallel()

	cases := map[string]ProjectDetails{
		"k8s · eu-west-1": {Infra: "k8s", Region: "eu-west-1"},
		"railway":         {Infra: "railway"},
		"eu-west-1":       {Region: "eu-west-1"},
		"":                {},
	}
	for want, details := range cases {
		if got := details.Place(); got != want {
			t.Errorf("%+v is said %q, want %q", details, got, want)
		}
	}
}

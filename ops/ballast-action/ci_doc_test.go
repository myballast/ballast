package ballastaction

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/youssefmahersi/ballast/internal/cli"
	"github.com/youssefmahersi/ballast/internal/domain"
)

// The page "Run Ballast from CI" is what somebody follows from nothing to a passing and a
// failing job, and what it says about limits, codes and the proof of a target is read as
// a promise. What these tests hold in place is that every number and every name in it is
// the code's own, so that changing a limit in the code turns this page red until it is
// said here too:
//
//   - the limits and the lifetimes of a token are the constants of the domain;
//   - the exit codes are the command's;
//   - the file that proves a target is where the verifier looks for it;
//   - the five steps of the way are all there, in order, and each link goes somewhere;
//   - nothing in it is a value that looks like a token.

const ciPage = "../../docs/ci.md"

func TestTheLimitsTheCIPageStatesAreTheLimitsOfTheCode(t *testing.T) {
	page := flat(read(t, ciPage))

	for _, want := range []string{
		fmt.Sprintf("| Virtual users | %d at most", domain.MaxVUsAnywhere),
		fmt.Sprintf("| Length of a run | %d hour at most", int(domain.MaxDurationAnywhere.Hours())),
		fmt.Sprintf("| Runs per hour | %d per token, %d for the organization", domain.MaxRunsPerUserPerWindow, domain.MaxRunsPerOrgPerWindow),
		fmt.Sprintf("| Runs waiting | %d for the organization", domain.MaxQueuedRunsPerOrg),
		fmt.Sprintf("after %d days unless you choose another time, one year at most", int(domain.APITokenDefaultTTL.Hours()/24)),
		fmt.Sprintf("A project holds %d live tokens", domain.MaxAPITokensPerProject),
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the page does not say %q", want)
		}
	}
	if int(domain.APITokenMaxTTL.Hours()/24) != 365 || domain.RateWindow.Hours() != 1 {
		t.Error("the window of the rate limit or the longest token changed: the page says an hour and a year")
	}
}

func TestTheExitCodesTheCIPageGivesAreTheCommands(t *testing.T) {
	page := flat(read(t, ciPage))

	for _, want := range []string{
		fmt.Sprintf("| %d | `met` |", cli.ExitMet),
		fmt.Sprintf("| %d | `breached` |", cli.ExitBreached),
		fmt.Sprintf("| %d | `none`, or empty |", cli.ExitError),
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the page does not say %q", want)
		}
	}
}

func TestTheFileThatProvesATargetIsWhereTheVerifierLooksForIt(t *testing.T) {
	environment := domain.Environment{BaseURL: "https://staging.example.com/app", VerificationToken: "abc_DEF-123"}
	proof, err := environment.ProofURL()
	if err != nil {
		t.Fatal(err)
	}
	path := strings.TrimPrefix(proof, "https://staging.example.com")
	if path != "/.well-known/ballast-abc_DEF-123" {
		t.Fatalf("the verifier looks for %q", path)
	}

	// The page writes the path with a placeholder for the token.
	if !strings.Contains(read(t, ciPage), strings.Replace(path, "abc_DEF-123", "<token>", 1)) {
		t.Errorf("the page does not give the path %s", path)
	}
}

func TestTheStepsOfTheWayAreAllThereAndInOrder(t *testing.T) {
	page := read(t, ciPage)

	steps := []string{
		"## 1. A scenario with a threshold",
		"## 2. A target Ballast may load",
		"## 3. A token, and where to put it",
		"## 4. Add the workflow",
		"## 5. Read the verdict",
	}
	last := -1
	for _, step := range steps {
		at := strings.Index(page, step)
		if at < 0 {
			t.Errorf("the page has no %q", step)
			continue
		}
		if at < last {
			t.Errorf("%q is out of order", step)
		}
		last = at
	}

	// What the ticket asks the page to say, in the page's own words.
	flatPage := strings.ToLower(flat(page))
	for _, want := range []string{
		"ci tokens", "create token", "once", "approves", "reachable from the internet", "no setting to allow",
		"private", "exit", "breached", "limits", "hosting ballast yourself", "fork", "met", "no checkout is needed",
		"pull_request_target", "administrator",
	} {
		if !strings.Contains(flatPage, want) {
			t.Errorf("the page does not say %q", want)
		}
	}
}

func TestTheCIPageHoldsNoValueThatLooksLikeAToken(t *testing.T) {
	if secretShape.MatchString(read(t, ciPage)) {
		t.Error("the page holds something that looks like a token")
	}
}

var secretShape = regexp.MustCompile(`blt_[A-Za-z0-9_-]{8,}`)

func TestEveryLinkOfTheCIPageGoesSomewhere(t *testing.T) {
	page := read(t, ciPage)

	for _, m := range regexp.MustCompile(`\]\(([^)]+)\)`).FindAllStringSubmatch(page, -1) {
		target := m[1]
		switch {
		case strings.HasPrefix(target, "https://"):
			if !strings.HasPrefix(target, "https://github.com/myballast/") {
				t.Errorf("a link leaves the project: %s", target)
			}
		case strings.HasPrefix(target, "#"):
			anchor := strings.TrimPrefix(target, "#")
			if !hasHeading(page, anchor) {
				t.Errorf("the link %s points at a heading that is not there", target)
			}
		default:
			if _, err := os.Stat("../../docs/" + strings.SplitN(target, "#", 2)[0]); err != nil {
				t.Errorf("the link %s goes nowhere", target)
			}
		}
	}
}

// hasHeading says whether a heading of the page has this GitHub anchor: lower case, the
// punctuation gone, spaces turned into hyphens.
func hasHeading(page, anchor string) bool {
	for _, line := range strings.Split(page, "\n") {
		text, ok := strings.CutPrefix(line, "#")
		if !ok {
			continue
		}
		text = strings.TrimLeft(text, "# ")
		var b strings.Builder
		for _, r := range strings.ToLower(text) {
			switch {
			case r == ' ' || r == '-':
				b.WriteRune('-')
			case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
				b.WriteRune(r)
			}
		}
		if b.String() == anchor {
			return true
		}
	}
	return false
}

func TestTheOtherPagesLeadToTheCIPageAndTheActionReadmeSaysWhereTheTargetMustBe(t *testing.T) {
	for _, page := range []string{"../../README.md", "../../docs/cli.md"} {
		if !strings.Contains(read(t, page), "ci.md") {
			t.Errorf("%s does not lead to the CI page", page)
		}
	}

	readme := flat(read(t, "README.md"))
	for _, want := range []string{"reachable from the internet", "no setting to allow one", "docs/ci.md"} {
		if !strings.Contains(readme, want) {
			t.Errorf("the README of the Action does not say %q", want)
		}
	}
}

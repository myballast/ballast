package ballastaction

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// What the Action's README and the page of the command say about the Action is read by
// somebody who is about to put a token in a workflow. These tests hold in place that it
// is true of action.yml:
//
//   - every example is a workflow that works with the Action as it is: it names inputs
//     that exist, gives every input that is required, and gives the token as a secret;
//   - every input and every output has a row in the README;
//   - what the page says about the workflow's own safety (permissions, the fork, passing
//     outputs through env:) is there, and so is the procedure of the maintainer.

func read(t *testing.T, path string) string {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

var fence = regexp.MustCompile("(?s)```yaml\n(.*?)```")

// examples are the workflows a page shows, parsed.
func examples(t *testing.T, path string) []map[string]any {
	t.Helper()

	var found []map[string]any
	for _, block := range fence.FindAllStringSubmatch(read(t, path), -1) {
		// A block that is a few steps and not a whole workflow is wrapped, so that it
		// is read the same way.
		text := block[1]
		if !strings.Contains(text, "jobs:") {
			text = "jobs:\n  example:\n    runs-on: ubuntu-latest\n    steps:\n" + indent(text, "    ")
		}
		var w map[string]any
		if err := yaml.Unmarshal([]byte(text), &w); err != nil {
			t.Fatalf("%s: an example is not YAML: %v\n%s", path, err, block[1])
		}
		found = append(found, w)
	}
	if len(found) == 0 {
		t.Fatalf("%s has no workflow example", path)
	}
	return found
}

func indent(text, prefix string) string {
	var out []string
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		out = append(out, prefix+line)
	}
	return strings.Join(out, "\n") + "\n"
}

// uses are the steps of an example that use the Action, with their `with`.
func uses(t *testing.T, w map[string]any) []map[string]any {
	t.Helper()

	var found []map[string]any
	jobs, _ := w["jobs"].(map[string]any)
	for _, j := range jobs {
		job, _ := j.(map[string]any)
		steps, _ := job["steps"].([]any)
		for _, s := range steps {
			step, _ := s.(map[string]any)
			if u, _ := step["uses"].(string); strings.HasPrefix(u, "myballast/ballast-action@") {
				found = append(found, step)
			}
		}
	}
	return found
}

func TestEveryExampleIsAWorkflowThatWorksWithTheActionAsItIs(t *testing.T) {
	a := theAction(t)

	for _, page := range []string{"README.md", "../../docs/cli.md", "../../docs/ci.md"} {
		seen := 0
		for _, w := range examples(t, page) {
			for _, step := range uses(t, w) {
				seen++
				with, _ := step["with"].(map[string]any)

				for name := range with {
					if _, ok := a.Inputs[name]; !ok {
						t.Errorf("%s: an example gives %q, which is not an input of the Action", page, name)
					}
				}
				for name, in := range a.Inputs {
					if _, given := with[name]; in.Required && !given {
						t.Errorf("%s: an example does not give %q, which is required", page, name)
					}
				}

				token, _ := with["token"].(string)
				if !strings.HasPrefix(token, "${{ secrets.") {
					t.Errorf("%s: an example does not give the token as a secret: %q", page, token)
				}
				if profile, _ := with["profile"].(string); profile == "" {
					t.Errorf("%s: an example has no profile", page)
				}
			}
		}
		if seen == 0 {
			t.Errorf("%s: no example uses the Action", page)
		}
	}
}

func TestTheExamplesAskForNoMoreThanTheyNeed(t *testing.T) {
	for _, page := range []string{"README.md", "../../docs/cli.md", "../../docs/ci.md"} {
		for _, w := range examples(t, page) {
			perms, _ := w["permissions"].(map[string]any)
			jobs, _ := w["jobs"].(map[string]any)
			for _, j := range jobs {
				job, _ := j.(map[string]any)
				steps, _ := job["steps"].([]any)
				for _, s := range steps {
					step, _ := s.(map[string]any)
					if u, _ := step["uses"].(string); strings.HasPrefix(u, "actions/checkout") {
						t.Errorf("%s: an example checks the repository out, which the Action does not need", page)
					}
					// An output is data from a server: it reaches a script through env.
					if run, _ := step["run"].(string); strings.Contains(run, "${{") {
						t.Errorf("%s: an example writes an expression into a script:\n%s", page, run)
					}
				}
			}
			if _, whole := w["on"]; whole && perms["contents"] != "read" {
				t.Errorf("%s: a whole workflow in an example does not start from contents: read", page)
			}
			if on, ok := w["on"].(map[string]any); ok {
				if _, bad := on["pull_request_target"]; bad {
					t.Errorf("%s: an example is triggered by pull_request_target", page)
				}
			}
		}
	}
}

func TestEveryInputAndEveryOutputHasARowInTheReadme(t *testing.T) {
	readme := read(t, "README.md")
	a := theAction(t)

	for name := range a.Inputs {
		if !strings.Contains(readme, "| `"+name+"` |") {
			t.Errorf("the README has no row for the input %s", name)
		}
	}
	for name := range a.Outputs {
		if !strings.Contains(readme, "| `"+name+"` |") {
			t.Errorf("the README has no row for the output %s", name)
		}
	}

	// And no row for one that is not there.
	row := regexp.MustCompile("(?m)^\\| `([a-z-]+)` \\|")
	var extra []string
	for _, m := range row.FindAllStringSubmatch(readme, -1) {
		_, isInput := a.Inputs[m[1]]
		_, isOutput := a.Outputs[m[1]]
		if !isInput && !isOutput {
			extra = append(extra, m[1])
		}
	}
	sort.Strings(extra)
	if len(extra) != 0 {
		t.Errorf("the README has rows for things the Action does not have: %v", extra)
	}
}

func TestTheDefaultsTheReadmeStatesAreTheOnesOfTheAction(t *testing.T) {
	readme := flat(read(t, "README.md"))
	a := theAction(t)

	for _, name := range []string{"allow-no-threshold", "no-fail-on-breach"} {
		if a.Inputs[name].Default != "false" || !strings.Contains(readme, "| `"+name+"` | no | `false` |") {
			t.Errorf("the README does not say %s is false by default", name)
		}
	}
	if !strings.Contains(readme, "| `timeout` | no | two hours |") {
		t.Error("the README does not give the default timeout")
	}
}

func TestThePagesSayWhatKeepsAWorkflowSafe(t *testing.T) {
	readme := flat(read(t, "README.md"))
	for _, want := range []string{
		"fork", "pull_request_target", "secret", "not a signature", "continue-on-error", "env:",
		"verified", "no checkout is needed", "commit hash", "`latest` does not exist",
	} {
		if !strings.Contains(strings.ToLower(readme), strings.ToLower(want)) {
			t.Errorf("the README does not say %q", want)
		}
	}

	cliPage := flat(read(t, "../../docs/cli.md"))
	for _, want := range []string{"GITHUB_OUTPUT", "GITHUB_STEP_SUMMARY", "fork", "ballast-action@v1"} {
		if !strings.Contains(cliPage, want) {
			t.Errorf("docs/cli.md does not say %q", want)
		}
	}
	// The exit codes are the ones of the command.
	for _, want := range []string{"`1` when a threshold was breached", "`2` for anything else"} {
		if !strings.Contains(readme, want) {
			t.Errorf("the README does not say %q", want)
		}
	}
}

func TestTheProcedureOfTheMaintainerSaysHowTheActionIsPublished(t *testing.T) {
	page := flat(read(t, "../../docs/releasing.md"))

	for _, want := range []string{
		"myballast/ballast-action", "--exclude '*_test.go'", "VERSION", "git tag -f v1", "ne se déplace jamais",
		"avant", "-rc.N", "règle de protection des tags",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("docs/releasing.md does not say %q", want)
		}
	}
}

func TestTheReadmeOfTheRepositoryLeadsToTheAction(t *testing.T) {
	readme := read(t, "../../README.md")
	if !strings.Contains(readme, "myballast/ballast-action") || !strings.Contains(readme, "ops/ballast-action/") {
		t.Error("the README does not lead to the Action and to its source")
	}
}

func flat(text string) string { return strings.Join(strings.Fields(text), " ") }

// --- The workflow of the Action's own repository --------------------------------------------------

func TestTheWorkflowOfTheActionsRepositoryIsAsNarrowAsTheirsAre(t *testing.T) {
	raw := read(t, ".github/workflows/install.yml")

	var w struct {
		On          map[string]any    `yaml:"on"`
		Permissions map[string]string `yaml:"permissions"`
		Jobs        map[string]struct {
			Permissions map[string]string `yaml:"permissions"`
			Steps       []actionStep      `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(raw), &w); err != nil {
		t.Fatal(err)
	}

	if len(w.Permissions) != 1 || w.Permissions["contents"] != "read" {
		t.Errorf("permissions are %v, want only contents: read", w.Permissions)
	}
	for _, trigger := range []string{"pull_request_target", "workflow_run", "issue_comment"} {
		if _, bad := w.On[trigger]; bad {
			t.Errorf("it is triggered by %s", trigger)
		}
	}
	for name, j := range w.Jobs {
		if len(j.Permissions) != 0 {
			t.Errorf("job %s widens the permissions: %v", name, j.Permissions)
		}
		for i, s := range j.Steps {
			if s.Uses != "" && !strings.HasPrefix(s.Uses, "actions/checkout@") {
				t.Errorf("step %d of %s uses %s: only actions/checkout is allowed", i+1, name, s.Uses)
			}
			if strings.Contains(s.Run, "${{") {
				t.Errorf("step %d of %s writes an expression into a script:\n%s", i+1, name, s.Run)
			}
		}
	}
	if strings.Contains(raw, "secrets.") {
		t.Error("it uses a secret: it has no business with one, it stops at the version")
	}
}

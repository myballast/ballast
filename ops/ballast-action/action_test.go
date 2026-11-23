package ballastaction

import (
	"bytes"
	"os"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/youssefmahersi/ballast/internal/cli"
)

// action.yml is the glue between a workflow and the two scripts, and it is the one file of
// the Action that cannot be run here. What can be checked about it is checked, and says
// in red what the policy is:
//
//   - it is a composite action that uses no other action: what runs on somebody's
//     runner is these scripts and the command they download, and nothing that is not in
//     this repository;
//   - nothing the workflow's context holds is written into a script. Every input reaches
//     the scripts through `env:`, which is how a value stays a value;
//   - the token is given to the step that runs the command and to no other, and the
//     step that downloads gets nothing but the version it was asked for;
//   - the outputs are the ones the command writes, and no others.

type action struct {
	Name    string           `yaml:"name"`
	Inputs  map[string]input `yaml:"inputs"`
	Outputs map[string]struct {
		Description string `yaml:"description"`
		Value       string `yaml:"value"`
	} `yaml:"outputs"`
	Runs struct {
		Using string       `yaml:"using"`
		Steps []actionStep `yaml:"steps"`
	} `yaml:"runs"`
}

type input struct {
	Description string `yaml:"description"`
	Required    bool   `yaml:"required"`
	Default     any    `yaml:"default"`
}

type actionStep struct {
	ID    string            `yaml:"id"`
	Name  string            `yaml:"name"`
	Uses  string            `yaml:"uses"`
	Run   string            `yaml:"run"`
	Shell string            `yaml:"shell"`
	Env   map[string]string `yaml:"env"`
}

func theAction(t *testing.T) action {
	t.Helper()

	raw, err := os.ReadFile("action.yml")
	if err != nil {
		t.Fatalf("there is no action.yml: %v", err)
	}
	var a action
	if err := yaml.Unmarshal(raw, &a); err != nil {
		t.Fatalf("action.yml is not YAML: %v", err)
	}
	return a
}

func step(t *testing.T, a action, id string) actionStep {
	t.Helper()

	for _, s := range a.Runs.Steps {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("there is no step %q", id)
	return actionStep{}
}

func TestItIsACompositeActionThatUsesNoOtherAction(t *testing.T) {
	a := theAction(t)

	if a.Runs.Using != "composite" {
		t.Errorf("it is %q, want a composite action: there is no runtime to keep up to date", a.Runs.Using)
	}
	if len(a.Runs.Steps) != 2 {
		t.Errorf("%d steps, want two: install, then run", len(a.Runs.Steps))
	}
	for i, s := range a.Runs.Steps {
		if s.Uses != "" {
			t.Errorf("step %d uses %s: what runs on a runner is this repository and the command, nothing else", i+1, s.Uses)
		}
		if s.Shell != "bash" {
			t.Errorf("step %d has shell %q: every step is bash, on the three systems, since the scripts are", i+1, s.Shell)
		}
	}
}

func TestNoValueOfTheContextIsWrittenIntoAScript(t *testing.T) {
	for i, s := range theAction(t).Runs.Steps {
		if strings.Contains(s.Run, "${{") {
			t.Errorf("step %d writes an expression into its script: a value of the context would become code. Pass it through env:\n%s", i+1, s.Run)
		}
	}
}

func TestEachStepRunsOneOfTheTwoScriptsAndNothingElse(t *testing.T) {
	a := theAction(t)
	want := map[string]string{
		"install": `bash "$GITHUB_ACTION_PATH/install.sh"`,
		"run":     `bash "$GITHUB_ACTION_PATH/run.sh"`,
	}
	for id, command := range want {
		if got := strings.TrimSpace(step(t, a, id).Run); got != command {
			t.Errorf("step %s runs %q, want %q", id, got, command)
		}
	}
}

func TestTheStepsAreInstallThenRun(t *testing.T) {
	a := theAction(t)

	if len(a.Runs.Steps) != 2 || a.Runs.Steps[0].ID != "install" || a.Runs.Steps[1].ID != "run" {
		t.Errorf("the steps are not install, then run: %+v", a.Runs.Steps)
	}
}

func TestTheTokenIsGivenToTheStepThatRunsTheCommandAndToNoOther(t *testing.T) {
	a := theAction(t)

	for _, s := range a.Runs.Steps {
		for name, value := range s.Env {
			holdsToken := name == "BALLAST_TOKEN" || strings.Contains(value, "inputs.token")
			if holdsToken && s.ID != "run" {
				t.Errorf("step %s is given the token (%s): only the step that runs the command needs it", s.ID, name)
			}
		}
	}
	if got := step(t, a, "run").Env["BALLAST_TOKEN"]; got != "${{ inputs.token }}" {
		t.Errorf("the run step's token is %q, want it to come from the token input", got)
	}
	// The download step gets what it needs and nothing more.
	if env := step(t, a, "install").Env; len(env) != 1 || env["BALLAST_VERSION"] != "${{ inputs.version }}" {
		t.Errorf("the install step's environment is %v, want only the version", env)
	}
}

func TestEveryInputReachesTheScriptThroughTheEnvironment(t *testing.T) {
	a := theAction(t)
	want := map[string]string{
		"BALLAST_TOKEN":                  "token",
		"BALLAST_URL":                    "url",
		"BALLAST_BIN":                    "", // the install step's output, below
		"BALLAST_ARG_SCENARIO":           "scenario",
		"BALLAST_ARG_ENVIRONMENT":        "environment",
		"BALLAST_ARG_PROFILE":            "profile",
		"BALLAST_ARG_TIMEOUT":            "timeout",
		"BALLAST_ARG_QUEUE_TIMEOUT":      "queue-timeout",
		"BALLAST_ARG_ALLOW_NO_THRESHOLD": "allow-no-threshold",
		"BALLAST_ARG_NO_FAIL_ON_BREACH":  "no-fail-on-breach",
	}
	env := step(t, a, "run").Env
	if len(env) != len(want) {
		t.Errorf("the run step has %d variables, want %d: %v", len(env), len(want), env)
	}
	for name, input := range want {
		expected := "${{ inputs." + input + " }}"
		if input == "" {
			expected = "${{ steps.install.outputs.bin }}"
		}
		if env[name] != expected {
			t.Errorf("%s is %q, want %q", name, env[name], expected)
		}
	}
}

func TestTheInputsAreTheOnesTheDocumentationPromises(t *testing.T) {
	a := theAction(t)

	required := map[string]bool{"token": true, "url": true, "scenario": true, "environment": true, "profile": true}
	for name, in := range a.Inputs {
		if in.Description == "" {
			t.Errorf("input %s has no description", name)
		}
		if required[name] != in.Required {
			t.Errorf("input %s: required is %v, want %v", name, in.Required, required[name])
		}
	}
	for name := range required {
		if _, ok := a.Inputs[name]; !ok {
			t.Errorf("there is no input %s", name)
		}
	}

	var names []string
	for name := range a.Inputs {
		names = append(names, name)
	}
	sort.Strings(names)
	want := "allow-no-threshold environment no-fail-on-breach profile queue-timeout scenario timeout token url version"
	if strings.Join(names, " ") != want {
		t.Errorf("inputs are %v, want %s", names, want)
	}

	// The two switches are off unless somebody turns them on, and the version is the
	// Action's own unless somebody names one.
	for _, name := range []string{"allow-no-threshold", "no-fail-on-breach"} {
		if a.Inputs[name].Default != "false" {
			t.Errorf("%s defaults to %v, want \"false\"", name, a.Inputs[name].Default)
		}
	}
	if d := a.Inputs["version"].Default; d != nil && d != "" {
		t.Errorf("version defaults to %v: the default is the VERSION file, so that @v1 always means a known command", d)
	}
}

func TestTheOutputsAreTheOnesTheCommandWritesAndNoOthers(t *testing.T) {
	a := theAction(t)

	// What the command writes is read from the command: a name added there and not here
	// is an output nobody can reach.
	var written bytes.Buffer
	if err := cli.WriteGitHubOutputs(&written, cli.Result{}); err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, line := range strings.Split(strings.TrimSpace(written.String()), "\n") {
		name, _, _ := strings.Cut(line, "=")
		want = append(want, name)
	}

	var got []string
	for name, out := range a.Outputs {
		got = append(got, name)
		if out.Value != "${{ steps.run.outputs."+name+" }}" {
			t.Errorf("output %s is %q, want it read from the run step", name, out.Value)
		}
		if out.Description == "" {
			t.Errorf("output %s has no description", name)
		}
	}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("outputs are %v, the command writes %v", got, want)
	}
}

func TestTheScriptsAreExecutableAndNothingElseIsHidingInTheDirectory(t *testing.T) {
	for _, name := range []string{"install.sh", "run.sh"} {
		info, err := os.Stat(name)
		if err != nil || info.Mode()&0o100 == 0 {
			t.Errorf("%s is missing or not executable: %v", name, err)
		}
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"action.yml": true, "install.sh": true, "run.sh": true, "VERSION": true, "README.md": true, ".github": true}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		if !allowed[e.Name()] {
			t.Errorf("%s is in the directory, and is not one of the files of the Action", e.Name())
		}
	}
}

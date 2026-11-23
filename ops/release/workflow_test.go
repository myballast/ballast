package release

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The organization allows three actions and no others, and a workflow that publishes can
// do harm that one that tests cannot. These tests read the workflows themselves, and say
// in red what the policy is:
//
//   - only the three allowed actions are used, by every workflow;
//   - nothing the repository does not control reaches a shell as text: a value of the
//     workflow's context is passed through an environment variable and quoted, never
//     written into a script, which is how a branch or tag name becomes a command;
//   - nothing privileged is triggered by what a stranger can write;
//   - the release has the narrowest rights it can do its job with — write access to
//     contents for the one job that publishes, read access for everything else — and
//     its token is given to the one step that needs it.
//
// The workflow cannot be run here. What can be checked about it is checked, and the rest
// is the first tag.

type workflow struct {
	Name        string            `yaml:"name"`
	On          map[string]any    `yaml:"on"`
	Permissions map[string]string `yaml:"permissions"`
	Concurrency struct {
		Group            string `yaml:"group"`
		CancelInProgress bool   `yaml:"cancel-in-progress"`
	} `yaml:"concurrency"`
	Jobs map[string]job `yaml:"jobs"`
}

type job struct {
	Name           string            `yaml:"name"`
	Uses           string            `yaml:"uses"`
	RunsOn         string            `yaml:"runs-on"`
	TimeoutMinutes int               `yaml:"timeout-minutes"`
	Permissions    map[string]string `yaml:"permissions"`
	Env            map[string]string `yaml:"env"`
	Steps          []step            `yaml:"steps"`
}

type step struct {
	Name string            `yaml:"name"`
	Uses string            `yaml:"uses"`
	Run  string            `yaml:"run"`
	With map[string]any    `yaml:"with"`
	Env  map[string]string `yaml:"env"`
}

var allowedActions = map[string]bool{
	"actions/checkout":   true,
	"actions/setup-go":   true,
	"actions/setup-node": true,
}

func workflowFiles(t *testing.T) map[string]workflow {
	t.Helper()

	paths, err := filepath.Glob("../../.github/workflows/*.y*ml")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no workflows were found (%v): this test would prove nothing", err)
	}

	found := map[string]workflow{}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		var w workflow
		if err := yaml.Unmarshal(raw, &w); err != nil {
			t.Fatalf("%s is not a workflow: %v", path, err)
		}
		found[filepath.Base(path)] = w
	}
	return found
}

func theRelease(t *testing.T) workflow {
	t.Helper()

	w, ok := workflowFiles(t)["release.yml"]
	if !ok {
		t.Fatal("there is no .github/workflows/release.yml")
	}
	return w
}

func onlyJob(t *testing.T, w workflow) job {
	t.Helper()

	if len(w.Jobs) != 1 {
		t.Fatalf("the release has %d jobs, want exactly one: a second job is a second set of rights and no way to hand it the files", len(w.Jobs))
	}
	for _, j := range w.Jobs {
		return j
	}
	return job{}
}

// --- Every workflow ------------------------------------------------------------------------------

func TestOnlyTheAllowedActionsAreUsed(t *testing.T) {
	for file, w := range workflowFiles(t) {
		for name, j := range w.Jobs {
			if j.Uses != "" {
				t.Errorf("%s: job %s uses a reusable workflow, %s, and the policy names three actions", file, name, j.Uses)
			}
			for i, s := range j.Steps {
				if s.Uses == "" {
					continue
				}
				action, ref, found := strings.Cut(s.Uses, "@")
				if !allowedActions[action] {
					t.Errorf("%s: step %d of %s uses %s, which the organization does not allow", file, i+1, name, s.Uses)
				}
				if !found || ref == "" {
					t.Errorf("%s: step %d of %s uses %s with no version", file, i+1, name, s.Uses)
				}
			}
		}
	}
}

// A value of the workflow's context — a branch, a tag, a title — written into a script is
// a value somebody else chose, run as code. Passed through an environment variable and
// quoted, it is a string.
func TestNoValueOfTheContextIsWrittenIntoAScript(t *testing.T) {
	for file, w := range workflowFiles(t) {
		for name, j := range w.Jobs {
			for i, s := range j.Steps {
				if strings.Contains(s.Run, "${{") {
					t.Errorf("%s: step %d of %s writes an expression into its script; pass it in env: and quote it:\n%s", file, i+1, name, s.Run)
				}
			}
		}
	}
}

func TestNothingPrivilegedIsTriggeredByWhatAStrangerCanWrite(t *testing.T) {
	for file, w := range workflowFiles(t) {
		for _, trigger := range []string{"pull_request_target", "workflow_run", "issue_comment", "issues", "discussion", "discussion_comment"} {
			if _, present := w.On[trigger]; present {
				t.Errorf("%s is triggered by %s", file, trigger)
			}
		}
	}
}

func TestEveryWorkflowStartsFromReadOnly(t *testing.T) {
	for file, w := range workflowFiles(t) {
		if w.Permissions["contents"] != "read" {
			t.Errorf("%s: the workflow does not start from contents: read (it says %v)", file, w.Permissions)
		}
		for name, j := range w.Jobs {
			for scope, level := range j.Permissions {
				if level == "write" && (file != "release.yml" || scope != "contents") {
					t.Errorf("%s: job %s asks for write access to %s", file, name, scope)
				}
			}
		}
	}
}

// --- The release ---------------------------------------------------------------------------------

func TestAReleaseIsOnlyStartedByAVersionTag(t *testing.T) {
	w := theRelease(t)

	if len(w.On) != 1 {
		t.Errorf("the release has triggers %v, want a push of tags and nothing else: a manual run is a release from any commit", w.On)
	}
	push, ok := w.On["push"].(map[string]any)
	if !ok {
		t.Fatalf("the release is not triggered by a push: %v", w.On)
	}
	if _, branches := push["branches"]; branches {
		t.Error("the release is triggered by branches")
	}
	tags, _ := push["tags"].([]any)
	if len(tags) == 0 {
		t.Fatal("the release is triggered by no tag filter: every tag would publish")
	}
	for _, pattern := range tags {
		if !strings.HasPrefix(fmt.Sprint(pattern), "v") {
			t.Errorf("the tag filter %v does not begin with v", pattern)
		}
	}
}

func TestTheReleaseHasOneJobWithTheNarrowestRightsItCanDoItsWorkWith(t *testing.T) {
	w := theRelease(t)
	j := onlyJob(t, w)

	if len(w.Permissions) != 1 || w.Permissions["contents"] != "read" {
		t.Errorf("the workflow's permissions are %v, want contents: read", w.Permissions)
	}
	if len(j.Permissions) != 1 || j.Permissions["contents"] != "write" {
		t.Errorf("the job's permissions are %v, want contents: write and nothing else", j.Permissions)
	}
	if j.RunsOn != "ubuntu-latest" {
		t.Errorf("runs on %q", j.RunsOn)
	}
	if j.TimeoutMinutes <= 0 || j.TimeoutMinutes > 60 {
		t.Errorf("timeout-minutes is %d: a release that hangs holds a token that can write", j.TimeoutMinutes)
	}
}

// Two releases of the same tag at once would race to create the same draft.
func TestTwoRunsOfTheSameTagDoNotRaceAndNoneIsCancelled(t *testing.T) {
	w := theRelease(t)

	if !strings.Contains(w.Concurrency.Group, "github.ref") {
		t.Errorf("the concurrency group %q does not depend on the tag", w.Concurrency.Group)
	}
	if w.Concurrency.CancelInProgress {
		t.Error("a release in progress is cancelled by a newer one: it would be left half-made")
	}
}

func TestTheReleaseIsBuiltFromTheWholeHistoryWithNothingCached(t *testing.T) {
	j := onlyJob(t, theRelease(t))

	var checkout, setupGo *step
	for i := range j.Steps {
		switch {
		case strings.HasPrefix(j.Steps[i].Uses, "actions/checkout@"):
			checkout = &j.Steps[i]
		case strings.HasPrefix(j.Steps[i].Uses, "actions/setup-go@"):
			setupGo = &j.Steps[i]
		}
	}
	if checkout == nil || setupGo == nil {
		t.Fatal("the release does not check out the source and set up Go")
	}
	// The history is what lets it be said whether the tag is on main.
	if fmt.Sprint(checkout.With["fetch-depth"]) != "0" {
		t.Errorf("fetch-depth is %v, want 0", checkout.With["fetch-depth"])
	}
	// The Go version is the one go.mod pins, the one the binaries are built with.
	if setupGo.With["go-version-file"] != "go.mod" {
		t.Errorf("go-version-file is %v, want go.mod", setupGo.With["go-version-file"])
	}
	// A cache that other branches can write to is a way for them to put something in
	// what is published.
	if fmt.Sprint(setupGo.With["cache"]) != "false" {
		t.Errorf("cache is %v, want false: a release does not read what a pull request could have written", setupGo.With["cache"])
	}
}

// The token reaches the one step that publishes, as the workflow's own token and in no
// other form: no secret of the repository is used, and nothing else can read it.
func TestTheTokenIsGivenToTheStepThatPublishesAndToNoOther(t *testing.T) {
	w := theRelease(t)
	j := onlyJob(t, w)

	if _, present := j.Env["GH_TOKEN"]; present {
		t.Error("the token is in the environment of the whole job")
	}
	var holders []string
	for i, s := range j.Steps {
		if value, present := s.Env["GH_TOKEN"]; present {
			holders = append(holders, s.Name)
			if value != "${{ github.token }}" {
				t.Errorf("step %d gets its token from %q, want ${{ github.token }}", i+1, value)
			}
			if !strings.Contains(s.Run, "publish.sh") {
				t.Errorf("step %d holds the token and does not publish", i+1)
			}
		}
	}
	if len(holders) != 1 {
		t.Errorf("the token is given to %d step(s) (%v), want exactly one", len(holders), holders)
	}

	raw, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(raw), "secrets.") {
		t.Error("the release uses a secret of the repository: the workflow's own token is all it needs")
	}
}

// Everything is checked before anything is built, and everything is built before anything
// is published; and a value that came from the tag is always quoted.
func TestTheReleaseChecksThenTestsThenBuildsThenPublishesInThatOrder(t *testing.T) {
	j := onlyJob(t, theRelease(t))

	order := []string{
		"build.sh --check-version",     // the tag is a version
		"git merge-base --is-ancestor", // and is on main
		"go test",                      // the tool and the domain pass their tests
		"build.sh",                     // the binaries
		"publish.sh",                   // and they are published
	}
	positions := make([]int, len(order))
	for i, needle := range order {
		positions[i] = -1
		for n, s := range j.Steps {
			// build.sh alone is the build; the check-version call is the first.
			if strings.Contains(s.Run, needle) && (needle != "build.sh" || !strings.Contains(s.Run, "--check-version")) {
				positions[i] = n
				break
			}
		}
		if positions[i] < 0 {
			t.Fatalf("no step runs %q", needle)
		}
	}
	for i := 1; i < len(positions); i++ {
		if positions[i] <= positions[i-1] {
			t.Errorf("%q (step %d) does not come after %q (step %d)", order[i], positions[i]+1, order[i-1], positions[i-1]+1)
		}
	}
	if last := len(j.Steps) - 1; positions[len(positions)-1] != last {
		t.Errorf("publishing is step %d of %d: nothing runs after it", positions[len(positions)-1]+1, last+1)
	}
}

func TestEveryUseOfTheTagIsThroughTheEnvironmentAndQuoted(t *testing.T) {
	j := onlyJob(t, theRelease(t))
	bare := regexp.MustCompile(`(^|[^"])\$\{?TAG\b`)

	used := 0
	for i, s := range j.Steps {
		if !strings.Contains(s.Run, "TAG") {
			continue
		}
		used++
		if s.Env["TAG"] != "${{ github.ref_name }}" {
			t.Errorf("step %d uses $TAG and does not get it from github.ref_name through env: %q", i+1, s.Env["TAG"])
		}
		if bare.MatchString(s.Run) {
			t.Errorf("step %d uses $TAG without quoting it:\n%s", i+1, s.Run)
		}
	}
	if used < 3 {
		t.Errorf("only %d step(s) use the tag: the check, the build and the publication all do", used)
	}
}

// A tag is somebody's choice; the release is the maintainer's. A tag on a commit that is
// not on main is a commit nobody reviewed.
func TestATagThatIsNotOnMainIsNotPublished(t *testing.T) {
	j := onlyJob(t, theRelease(t))

	for _, s := range j.Steps {
		if strings.Contains(s.Run, "merge-base") {
			if !strings.Contains(s.Run, "origin/main") || !strings.Contains(s.Run, "--is-ancestor") {
				t.Errorf("the check is not whether the tagged commit is an ancestor of origin/main:\n%s", s.Run)
			}
			// The commit the tag points to, whatever kind of tag it is: an annotated tag is an
			// object of its own, and asking about it would ask about the wrong thing.
			if !strings.Contains(s.Run, "^{commit}") {
				t.Errorf("the tag is not peeled to its commit:\n%s", s.Run)
			}
			if !strings.Contains(s.Run, "exit 1") {
				t.Errorf("a tag that is not on main does not stop the release:\n%s", s.Run)
			}
			return
		}
	}
	t.Fatal("no step checks that the tag is on main")
}

func TestTheReleaseRunsOnlyTheScriptsThatAreTested(t *testing.T) {
	j := onlyJob(t, theRelease(t))

	var run strings.Builder
	for _, s := range j.Steps {
		run.WriteString(s.Run + "\n")
	}
	for _, want := range []string{"ops/release/build.sh", "ops/release/publish.sh"} {
		if !strings.Contains(run.String(), want) {
			t.Errorf("the release does not run %s", want)
		}
	}
	// The release is not a place to start downloading and running things.
	for _, forbidden := range []string{"curl ", "wget ", "| sh", "| bash", "npm ", "pip ", "docker "} {
		if strings.Contains(run.String(), forbidden) {
			t.Errorf("the release runs %q: no tool is added to the job", forbidden)
		}
	}
}

package ballastaction

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The example repository (examples/ci-demo, published as myballast/ballast-demo) is what
// somebody copies and runs to see a job pass and a job fail. Its service and its scenarios
// are tested where they are; what is tested here is what depends on the Action:
//
//   - its workflow uses the Action with inputs that exist, with the token as a secret and
//     nothing wider than read access;
//   - the job that is meant to breach checks that it did, from the verdict, and passes the
//     verdict to its script through the environment;
//   - everything the workflow reads from the repository's settings is said in the
//     README, so that following it leaves nothing unset;
//   - the image is as small and as unprivileged as the page says.

const demo = "../../examples/ci-demo/"

type demoWorkflow struct {
	Permissions map[string]string `yaml:"permissions"`
	Jobs        map[string]struct {
		Steps []struct {
			ID              string            `yaml:"id"`
			Uses            string            `yaml:"uses"`
			Run             string            `yaml:"run"`
			With            map[string]string `yaml:"with"`
			Env             map[string]string `yaml:"env"`
			ContinueOnError bool              `yaml:"continue-on-error"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

func theDemoWorkflow(t *testing.T) demoWorkflow {
	t.Helper()

	var w demoWorkflow
	if err := yaml.Unmarshal([]byte(read(t, demo+".github/workflows/load-test.yml")), &w); err != nil {
		t.Fatal(err)
	}
	return w
}

func TestTheDemoWorkflowUsesTheActionAsItIs(t *testing.T) {
	a := theAction(t)
	w := theDemoWorkflow(t)

	if len(w.Permissions) != 1 || w.Permissions["contents"] != "read" {
		t.Errorf("permissions are %v, want only contents: read", w.Permissions)
	}
	if len(w.Jobs) != 2 {
		t.Fatalf("%d jobs, want the two of the example", len(w.Jobs))
	}

	scenarios := map[string]bool{}
	for name, job := range w.Jobs {
		used := 0
		for _, s := range job.Steps {
			if s.Uses == "" {
				if strings.Contains(s.Run, "${{") {
					t.Errorf("job %s writes an expression into a script", name)
				}
				continue
			}
			used++
			if s.Uses != "myballast/ballast-action@v1" {
				t.Errorf("job %s uses %s, want myballast/ballast-action@v1 and nothing else", name, s.Uses)
			}
			for input := range s.With {
				if _, ok := a.Inputs[input]; !ok {
					t.Errorf("job %s gives %q, which is not an input of the Action", name, input)
				}
			}
			for input, spec := range a.Inputs {
				if _, given := s.With[input]; spec.Required && !given {
					t.Errorf("job %s does not give %q, which is required", name, input)
				}
			}
			if s.With["token"] != "${{ secrets.BALLAST_TOKEN }}" {
				t.Errorf("job %s gives the token as %q", name, s.With["token"])
			}
			scenarios[s.With["scenario"]] = true
		}
		if used != 1 {
			t.Errorf("job %s uses the Action %d times, want once", name, used)
		}
	}
	if len(scenarios) != 2 {
		t.Errorf("the two jobs run %d scenario(s), want two different ones", len(scenarios))
	}
}

func TestTheJobThatBreachesChecksThatItDidFromTheVerdict(t *testing.T) {
	job := theDemoWorkflow(t).Jobs["breaches"]
	if len(job.Steps) != 2 {
		t.Fatalf("%d steps, want the run and the check", len(job.Steps))
	}

	run, check := job.Steps[0], job.Steps[1]
	if !run.ContinueOnError || run.ID == "" {
		t.Errorf("the run step must have an id and continue-on-error: %+v", run)
	}
	if check.Env["VERDICT"] != "${{ steps."+run.ID+".outputs.verdict }}" {
		t.Errorf("the check reads %q: the verdict has to reach the script through the environment", check.Env["VERDICT"])
	}
	if !strings.Contains(check.Run, `"$VERDICT" != "breached"`) || !strings.Contains(check.Run, "exit 1") {
		t.Errorf("the check does not fail unless the verdict is breached:\n%s", check.Run)
	}

	// The other job has no such line: a breach there is a failure.
	for _, s := range theDemoWorkflow(t).Jobs["holds"].Steps {
		if s.ContinueOnError {
			t.Error("the job that must hold ignores errors")
		}
	}
}

func TestEverythingTheDemoWorkflowReadsIsSaidInItsReadme(t *testing.T) {
	workflow := read(t, demo+".github/workflows/load-test.yml")
	readme := read(t, demo+"README.md")

	names := regexp.MustCompile(`(?:vars|secrets)\.([A-Z_]+)`).FindAllStringSubmatch(workflow, -1)
	if len(names) < 5 {
		t.Fatalf("only %d settings were found in the workflow", len(names))
	}
	for _, m := range names {
		if !strings.Contains(readme, "`"+m[1]+"`") {
			t.Errorf("the README does not say what %s is", m[1])
		}
	}
}

func TestTheThresholdTheReadmeAndTheScenariosGiveIsTheSameOne(t *testing.T) {
	for _, file := range []string{"README.md", "scenarios/holds.js", "scenarios/breaches.js"} {
		text := read(t, demo+file)
		if !strings.Contains(text, "http_req_duration") || !strings.Contains(text, "p(95)<500") {
			t.Errorf("%s does not give the threshold http_req_duration p(95)<500", file)
		}
	}
	if !strings.Contains(read(t, demo+"README.md"), "/.well-known/ballast-") {
		t.Error("the README does not say where the proof of the target is served")
	}
}

func TestTheImageIsStaticSmallAndRunByNobody(t *testing.T) {
	file := read(t, demo+"Dockerfile")

	for _, want := range []string{"CGO_ENABLED=0", "FROM scratch", "USER 65534:65534", "EXPOSE 8080", "ENTRYPOINT"} {
		if !strings.Contains(file, want) {
			t.Errorf("the Dockerfile lacks %q", want)
		}
	}
	for _, bad := range []string{"ADD http", "curl ", "wget ", "--privileged", "USER root", ":latest"} {
		if strings.Contains(file, bad) {
			t.Errorf("the Dockerfile has %q", bad)
		}
	}
	// What is built is the Go version of this repository, which the service is tested with.
	if !strings.Contains(file, "golang:1.23") {
		t.Error("the Dockerfile does not build with Go 1.23, the version go.mod names")
	}
}

func TestTheDemoRepositoryHoldsNothingButTheDemo(t *testing.T) {
	entries, err := os.ReadDir(strings.TrimSuffix(demo, "/"))
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{
		"main.go": true, "server.go": true, "server_test.go": true, "Dockerfile": true,
		"README.md": true, "scenarios": true, ".github": true,
	}
	for _, e := range entries {
		if !allowed[e.Name()] {
			t.Errorf("%s is in the example, and is not one of its files", e.Name())
		}
	}
}

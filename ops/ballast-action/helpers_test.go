// Package ballastaction holds the tests of the GitHub Action that sits next to them.
//
// The directory is the content of the repository that publishes the Action,
// myballast/ballast-action, apart from the tests: they run here, in `make test`, where
// the CLI and the release scripts they are checked against live. Copying the directory
// without the _test.go files is how the Action is published (docs/releasing.md).
package ballastaction

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type result struct {
	out  string
	code int
}

func here(t *testing.T, name string) string {
	t.Helper()

	path, err := filepath.Abs(name)
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	return path
}

// cleanEnvironment is this process's environment without anything the scripts read, so
// that what a developer has set in their own shell cannot make a test pass or fail.
func cleanEnvironment() []string {
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "BALLAST_") || strings.HasPrefix(name, "RUNNER_") ||
			strings.HasPrefix(name, "GITHUB_") || strings.HasPrefix(name, "INPUT_") {
			continue
		}
		env = append(env, kv)
	}
	return env
}

// runScript runs one of the Action's scripts from a directory that is never the
// repository: a script that only works from its own directory is one that breaks in a
// workflow.
func runScript(t *testing.T, script string, env []string, args ...string) result {
	t.Helper()

	cmd := exec.Command("bash", append([]string{here(t, script)}, args...)...)
	cmd.Dir = t.TempDir()
	cmd.Env = append(cleanEnvironment(), env...)

	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out

	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return result{out: out.String()}
	case errors.As(err, &exit):
		return result{out: out.String(), code: exit.ExitCode()}
	}
	t.Fatalf("run %s: %v", script, err)
	return result{}
}

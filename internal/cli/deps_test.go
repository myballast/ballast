package cli

import (
	"os/exec"
	"strings"
	"testing"
)

// The tool is a client of the API and nothing else. It must not reach into the
// server's adapters, its infrastructure or its database driver: it would then ship
// to somebody's pipeline with code that has no business there, and a change to the
// server would change what the pipeline runs.
//
// The test reads what the package really depends on, transitively, from the
// toolchain, rather than what a linter was told to look for.
func TestTheToolDependsOnNothingOfTheServer(t *testing.T) {
	// The package, and the binary built from it: the binary is what ships, and a
	// file added next to main.go would otherwise be outside the check.
	for _, pattern := range []string{".", "../../cmd/ballast"} {
		t.Run(pattern, func(t *testing.T) { checkDependencies(t, pattern) })
	}
}

func checkDependencies(t *testing.T, pattern string) {
	t.Helper()

	out, err := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}}", pattern).Output()
	if err != nil {
		t.Fatalf("go list %s: %v", pattern, err)
	}

	deps := strings.Fields(string(out))
	if len(deps) < 20 {
		t.Fatalf("only %d dependencies were read: this test would prove nothing", len(deps))
	}

	forbidden := []string{
		"github.com/youssefmahersi/ballast/internal/adapter",
		"github.com/youssefmahersi/ballast/internal/infra",
		"github.com/youssefmahersi/ballast/internal/usecase",
		"github.com/jackc/pgx",
		"github.com/docker/docker",
		"net/http/httptest",
	}
	for _, dep := range deps {
		for _, bad := range forbidden {
			if dep == bad || strings.HasPrefix(dep, bad+"/") {
				t.Errorf("the tool depends on %s", dep)
			}
		}
	}
}

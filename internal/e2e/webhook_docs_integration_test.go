//go:build integration

package e2e

import (
	"context"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"
)

// The page "Start a run from a signed request" shows a shell example. An example that is
// not run is one that rots: this runs the one on the page, as it is written, against the
// real API, and checks that Ballast accepts what it sends and refuses what it sends with
// another secret.

// theExample is the first shell block of the page.
func theExample(t *testing.T) string {
	t.Helper()

	page, err := os.ReadFile("../../docs/webhook.md")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile("(?s)```sh\n(.*?)```").FindSubmatch(page)
	if m == nil {
		t.Fatal("the page has no shell example")
	}
	return string(m[1])
}

func runTheExample(t *testing.T, script string, env ...string) (string, error) {
	t.Helper()

	cmd := exec.Command("sh", "-c", script)
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH")}, env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestTheShellExampleOfThePageStartsARun(t *testing.T) {
	for _, tool := range []string{"openssl", "curl"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not installed", tool)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newCIWorld(ctx, t)
	hook := w.newWebhook(t)
	script := theExample(t)

	environment := func(secret string) []string {
		return []string{
			"SCENARIO_ID=" + w.a.scenarioID.String(), "ENVIRONMENT_ID=" + w.a.environmentID.String(),
			"BALLAST_WEBHOOK_SECRET=" + secret, "BALLAST_URL=" + w.api.URL, "BALLAST_WEBHOOK_PATH=" + hook.path,
		}
	}

	out, err := runTheExample(t, script, environment(hook.secret)...)
	if err != nil || !strings.Contains(out, `"status":"queued"`) {
		t.Fatalf("the example did not start a run: %v\n%s", err, out)
	}
	if n := w.runs(ctx, t); n != 1 {
		t.Errorf("%d runs after the example, want 1", n)
	}

	// With the wrong secret it is refused, and the page says why: nothing starts.
	out, err = runTheExample(t, script, environment("blw_not-the-secret-0123456789abcdefghijklmnopq")...)
	if err == nil || !strings.Contains(out, "was not accepted") {
		t.Errorf("the example with a wrong secret: %v\n%s", err, out)
	}
	if n := w.runs(ctx, t); n != 1 {
		t.Errorf("%d runs after a refused example, want 1", n)
	}
}

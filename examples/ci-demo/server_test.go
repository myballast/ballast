package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// The demo service is the target of the example: small, public, and nothing it does is
// a surprise. What these tests hold in place:
//
//   - it answers the three things the scenarios ask for, and /slow takes as long as it is
//     told to, within a limit, so that a threshold can be held or breached on demand;
//   - it serves the challenge that proves a target is yours, at the one path Ballast
//     reads, with the token as its whole content, and serves nothing under /.well-known
//     for any other name;
//   - what is typed in a query is a number or it is refused: nothing is echoed back;
//   - the scenarios are scripts that really run against it: the one that holds its
//     threshold does, the one that breaches it does.
//
// Written before the service, which is why they do not compile yet.

const aToken = "Zm9vYmFyLWRlbW8_dG9rZW4-0123456789"

func serve(t *testing.T, token string) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(NewHandler(token))
	t.Cleanup(server.Close)
	return server
}

func get(t *testing.T, url string) (int, string, http.Header) {
	t.Helper()

	res, err := http.Get(url) //nolint:gosec // the URL is the test server's
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	body, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(body), res.Header
}

func TestItAnswersHealthAndItems(t *testing.T) {
	server := serve(t, "")

	if status, body, _ := get(t, server.URL+"/health"); status != 200 || strings.TrimSpace(body) != "ok" {
		t.Errorf("/health: %d %q", status, body)
	}

	status, body, header := get(t, server.URL+"/items")
	if status != 200 || !strings.HasPrefix(header.Get("Content-Type"), "application/json") {
		t.Fatalf("/items: %d, %s", status, header.Get("Content-Type"))
	}
	var parsed struct {
		Count int              `json:"count"`
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil || parsed.Count != len(parsed.Items) || parsed.Count == 0 {
		t.Errorf("/items: %v, %+v", err, parsed)
	}
}

func TestSlowTakesAsLongAsItIsToldWithinALimit(t *testing.T) {
	server := serve(t, "")

	started := time.Now()
	status, _, _ := get(t, server.URL+"/slow?ms=120")
	took := time.Since(started)

	if status != 200 || took < 120*time.Millisecond || took > 2*time.Second {
		t.Errorf("/slow?ms=120: %d in %s", status, took)
	}

	// More than the limit is refused, and does not hold a connection for a minute.
	started = time.Now()
	status, _, _ = get(t, server.URL+"/slow?ms=60000")
	if status != http.StatusBadRequest || time.Since(started) > time.Second {
		t.Errorf("/slow?ms=60000: %d in %s, want a refusal at once", status, time.Since(started))
	}
}

func TestWhatIsTypedInTheQueryIsANumberOrItIsRefusedAndNeverEchoed(t *testing.T) {
	server := serve(t, "")

	for name, query := range map[string]string{
		"nothing":   "",
		"empty":     "?ms=",
		"negative":  "?ms=-1",
		"text":      "?ms=abc",
		"html":      "?ms=%3Cscript%3Ealert(1)%3C/script%3E",
		"float":     "?ms=1.5",
		"hex":       "?ms=0x10",
		"twice":     "?ms=1&ms=2",
		"too large": "?ms=99999999999999999999",
		"plus":      "?ms=%2B5",
		"spaces":    "?ms=%205",
	} {
		t.Run(name, func(t *testing.T) {
			status, body, _ := get(t, server.URL+"/slow"+query)

			if status != http.StatusBadRequest {
				t.Errorf("status %d, want 400", status)
			}
			for _, echoed := range []string{"script", "abc", "0x10"} {
				if strings.Contains(body, echoed) {
					t.Errorf("the answer repeats what was typed: %q", body)
				}
			}
		})
	}
}

func TestTheChallengeIsServedAtTheOnePathAndWithTheTokenAsItsWholeContent(t *testing.T) {
	server := serve(t, aToken)

	status, body, _ := get(t, server.URL+"/.well-known/ballast-"+aToken)
	if status != 200 || body != aToken {
		t.Errorf("the challenge: %d %q, want 200 and exactly the token", status, body)
	}

	for name, path := range map[string]string{
		"another token":    "/.well-known/ballast-other",
		"a prefix":         "/.well-known/ballast-" + aToken[:10],
		"a longer token":   "/.well-known/ballast-" + aToken + "x",
		"no prefix":        "/.well-known/" + aToken,
		"the directory":    "/.well-known/",
		"anything else":    "/.well-known/security.txt",
		"a trailing slash": "/.well-known/ballast-" + aToken + "/",
		"upper case":       "/.well-known/BALLAST-" + aToken,
	} {
		t.Run(name, func(t *testing.T) {
			if status, body, _ := get(t, server.URL+path); status != http.StatusNotFound || strings.Contains(body, aToken) {
				t.Errorf("%s: %d %q, want 404 and nothing of the token", path, status, body)
			}
		})
	}
}

func TestWithoutATokenNoChallengeIsServedAndNothingIsGuessed(t *testing.T) {
	server := serve(t, "")

	for _, path := range []string{"/.well-known/ballast-", "/.well-known/ballast-x", "/.well-known/ballast"} {
		if status, _, _ := get(t, server.URL+path); status != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", path, status)
		}
	}
}

func TestATokenThatIsNotAPathSegmentIsNeverServed(t *testing.T) {
	for _, token := range []string{"a/b", "../x", "a b", "a\nb", "é", strings.Repeat("a", 129), "a?b", "a%2fb"} {
		if _, err := newChallenge(token); err == nil {
			t.Errorf("the token %q was accepted", token)
		}
	}
	for _, token := range []string{"", "abc", aToken, strings.Repeat("a", 128)} {
		if _, err := newChallenge(token); err != nil {
			t.Errorf("the token %q was refused: %v", token, err)
		}
	}
}

func TestOnlyReadingIsAllowedAndNothingIsSaid(t *testing.T) {
	server := serve(t, aToken)

	for _, path := range []string{"/health", "/items", "/slow?ms=1", "/.well-known/ballast-" + aToken} {
		res, err := http.Post(server.URL+path, "text/plain", strings.NewReader("x")) //nolint:gosec // the test server's
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		if res.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("POST %s: %d, want 405", path, res.StatusCode)
		}
	}

	if status, _, _ := get(t, server.URL+"/nothing-here"); status != http.StatusNotFound {
		t.Errorf("an unknown path: %d, want 404", status)
	}
	_, _, header := get(t, server.URL+"/health")
	if header.Get("Server") != "" || header.Get("X-Powered-By") != "" {
		t.Errorf("the answer names the software: %v", header)
	}
}

func TestThePortComesFromTheEnvironmentAndNothingElseIsConfigurable(t *testing.T) {
	for env, want := range map[string]string{"": ":8080", "9090": ":9090", "0": ":8080", "abc": ":8080", "70000": ":8080", "-1": ":8080"} {
		if got := listenAddress(env); got != want {
			t.Errorf("PORT=%q: %s, want %s", env, got, want)
		}
	}
}

// --- The scenarios -------------------------------------------------------------------------------------

func scenarioPath(name string) string { return filepath.Join("scenarios", name) }

func TestTheScenariosAskOnlyForWhatTheServiceAnswers(t *testing.T) {
	server := serve(t, "")
	request := regexp.MustCompile(`\$\{__ENV\.BASE_URL\}(/[A-Za-z0-9_/?=.&-]*)`)

	for _, name := range []string{"holds.js", "breaches.js"} {
		raw, err := os.ReadFile(scenarioPath(name))
		if err != nil {
			t.Fatal(err)
		}
		script := string(raw)

		matches := request.FindAllStringSubmatch(script, -1)
		if len(matches) == 0 {
			t.Fatalf("%s asks for nothing", name)
		}
		for _, m := range matches {
			if status, _, _ := get(t, server.URL+m[1]); status != 200 {
				t.Errorf("%s asks for %s, which answers %d", name, m[1], status)
			}
		}

		// The address of the target comes from Ballast, and the load from its profile.
		if strings.Contains(script, "http://") || strings.Contains(script, "https://") {
			t.Errorf("%s holds an address: the target is BASE_URL", name)
		}
		if strings.Contains(script, "export const options") {
			t.Errorf("%s sets options: Ballast writes the load from the profile and the thresholds from the scenario", name)
		}
	}
}

// p95 runs a scenario with k6 against the service, a few times, and reads the 95th
// percentile of the duration of its requests.
func p95(t *testing.T, script string) float64 {
	t.Helper()

	server := serve(t, "")
	summary := filepath.Join(t.TempDir(), "summary.json")

	cmd := exec.Command("k6", "run", "--quiet", "--vus", "1", "--iterations", "4",
		"--summary-export", summary, "-e", "BASE_URL="+server.URL, scenarioPath(script))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("k6 run %s: %v\n%s", script, err, out)
	}

	raw, err := os.ReadFile(summary)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Metrics map[string]map[string]float64 `json:"metrics"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	return parsed.Metrics["http_req_duration"]["p(95)"]
}

func TestTheScenarioThatHoldsHoldsAndTheOneThatBreachesBreaches(t *testing.T) {
	if _, err := exec.LookPath("k6"); err != nil {
		t.Skip("k6 is not installed")
	}

	// The threshold of the example is p(95)<500 on http_req_duration.
	if held := p95(t, "holds.js"); held >= 500 {
		t.Errorf("the scenario that must hold has a p95 of %.0f ms", held)
	}
	if breached := p95(t, "breaches.js"); breached < 500 {
		t.Errorf("the scenario that must breach has a p95 of %.0f ms", breached)
	}
}

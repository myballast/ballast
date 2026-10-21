package k6_test

import (
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/youssefmahersi/ballast/internal/adapter/k6"
	"github.com/youssefmahersi/ballast/internal/domain"
)

// hostile is everything somebody could type into a field on the canvas to make
// the generated script do something the scenario never described.
var hostile = map[string]string{
	"a quote":            `a " b`,
	"a backslash":        `a \ b`,
	"a line break":       "a\nb",
	"a carriage return":  "a\rb",
	"a substitution":     "${__ENV.BALLAST_SECRET_KEY}",
	"a backtick":         "`whoami`",
	"a statement":        `"; fail("owned"); //`,
	"a comment":          "a // b",
	"a block comment":    "a /* b */ c",
	"a closing brace":    "} ); fail('owned'); (function(){",
	"a unicode escape":   `"`,
	"a null byte":        "a\x00b",
	"a script tag":       "</script>",
	"a template literal": "${1+1}",
}

// A value nobody checked ends up in four places in the script: a path, a
// header, a body and a variable. Each one is a different way of escaping into
// code, so each one is tried.
//
// The script is run, not read: a test that greps for backslashes proves that
// this generator escapes the way this test expects, which is not the same as
// proving nothing executes.
func TestAHostileValueCannotEscapeIntoCode(t *testing.T) {
	node(t)

	for what, value := range hostile {
		t.Run(what, func(t *testing.T) {
			graph := domain.ScenarioGraph{
				Start: "set",
				Nodes: []domain.Node{
					{ID: "set", Kind: domain.NodeSetVariable, Variable: "payload", Value: value},
					{
						ID: "call", Kind: domain.NodeHTTPRequest,
						Method:  "POST",
						Path:    "/things/{payload}",
						Body:    value,
						Headers: map[string]string{"X-Thing": value},
					},
					{ID: "done", Kind: domain.NodeEnd},
				},
				Edges: []domain.Edge{
					{From: "set", To: "call"},
					{From: "call", To: "done"},
				},
			}

			script, err := k6.GenerateScript(graph)
			if err != nil {
				t.Fatalf("GenerateScript: %v", err)
			}

			mustParse(t, script)

			// Run it. Reading the script would only prove that it escapes the
			// way this test expects, which is not the same as proving that
			// nothing executes.
			calls := runScript(t, script)
			if len(calls) != 1 {
				t.Fatalf("the script made %d request(s), wanted 1", len(calls))
			}

			// The value arrives whole, as data, in every place it was put.
			if calls[0].Body != value {
				t.Errorf("the body arrived as %q, wanted %q", calls[0].Body, value)
			}
			if calls[0].Headers["X-Thing"] != value {
				t.Errorf("the header arrived as %q, wanted %q", calls[0].Headers["X-Thing"], value)
			}
			// Decoded rather than re-encoded here: encodeURIComponent and Go's
			// escaping disagree on which characters need a percent, and the
			// question is whether the value survived, not how it was spelled.
			inPath := strings.TrimPrefix(calls[0].URL, "http://target.test/things/")
			decoded, err := url.PathUnescape(inPath)
			if err != nil {
				t.Fatalf("the address is not decodable: %q", calls[0].URL)
			}
			if decoded != value {
				t.Errorf("the address carried %q, wanted %q", decoded, value)
			}
		})
	}
}

// The same graph, built in three different orders, has to produce the same
// bytes. Without that, every save would look like a change and a reviewer could
// not tell a real edit from a reshuffle.
func TestTheSameGraphAlwaysProducesTheSameScript(t *testing.T) {
	first := completeGraph()

	second := completeGraph()
	reverse(second.Nodes)

	third := completeGraph()
	reverse(third.Edges)

	one, err := k6.GenerateScript(first)
	if err != nil {
		t.Fatalf("GenerateScript: %v", err)
	}

	for name, graph := range map[string]domain.ScenarioGraph{"nodes reversed": second, "edges reversed": third} {
		other, err := k6.GenerateScript(graph)
		if err != nil {
			t.Fatalf("GenerateScript with %s: %v", name, err)
		}
		if other != one {
			t.Errorf("with %s, the script differs", name)
		}
	}
}

// Headers live in a map, whose order Go deliberately shuffles. Sorting them is
// what keeps the script stable across runs of the same build.
func TestHeaderOrderDoesNotChangeTheScript(t *testing.T) {
	graph := func() domain.ScenarioGraph {
		return domain.ScenarioGraph{
			Start: "call",
			Nodes: []domain.Node{
				{
					ID: "call", Kind: domain.NodeHTTPRequest, Method: "GET", Path: "/items",
					Headers: map[string]string{"B": "2", "A": "1", "C": "3", "D": "4", "E": "5"},
				},
				{ID: "done", Kind: domain.NodeEnd},
			},
			Edges: []domain.Edge{{From: "call", To: "done"}},
		}
	}

	first, err := k6.GenerateScript(graph())
	if err != nil {
		t.Fatalf("GenerateScript: %v", err)
	}

	for i := 0; i < 20; i++ {
		again, err := k6.GenerateScript(graph())
		if err != nil {
			t.Fatalf("GenerateScript: %v", err)
		}
		if again != first {
			t.Fatal("two runs of the same graph produced different scripts")
		}
	}
}

// reverse flips a slice in place, which is the cheapest way to say "a different
// insertion order".
func reverse[T any](items []T) {
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
}

// mustParse checks that what was generated is valid JavaScript.
//
// node --check parses without running and without resolving the k6 imports,
// which is exactly the question being asked: is this a script, or is it broken
// text that happens to look like one?
func mustParse(t *testing.T, script string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "generated.mjs")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatalf("write the script: %v", err)
	}

	output, err := exec.Command("node", "--check", path).CombinedOutput()
	if err != nil {
		t.Fatalf("the generated script is not valid JavaScript: %v\n%s\n--- script ---\n%s",
			err, output, script)
	}
}

// node skips a test when Node is not installed, rather than failing for a
// reason that has nothing to do with the code.
func node(t *testing.T) {
	t.Helper()

	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed; it is what checks the generated script parses")
	}
}

// call is one HTTP request the generated script made, as the stub saw it.
type call struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Body    string            `json:"body"`
	Headers map[string]string `json:"headers"`
}

// runScript executes the generated script with k6 replaced by stubs, and
// returns the requests it made.
//
// The imports are stripped and the module's own default function is called, so
// what runs is exactly the body this generator wrote. If a value escaped into
// code, it runs here — and either it throws, or it shows up somewhere the test
// does not expect it.
func runScript(t *testing.T, script string) []call {
	t.Helper()

	var body strings.Builder
	for _, line := range strings.Split(script, "\n") {
		if strings.HasPrefix(line, "import ") {
			continue
		}
		body.WriteString(strings.Replace(line, "export default function ()", "const scenario = function ()", 1))
		body.WriteString("\n")
	}

	harness := `const calls = [];
const __ENV = { BASE_URL: "http://target.test", API_TOKEN: "s3cr3t-value" };
const http = {
  request(method, url, requestBody, params) {
    calls.push({
      method,
      url,
      body: requestBody === null ? "" : String(requestBody),
      headers: (params && params.headers) || {},
    });
    return { status: 200, json: () => null };
  },
};
const check = () => true;
const fail = (message) => { throw new Error("fail() was called: " + message); };
const sleep = () => {};

` + body.String() + `
scenario();
console.log(JSON.stringify(calls));
`

	path := filepath.Join(t.TempDir(), "harness.mjs")
	if err := os.WriteFile(path, []byte(harness), 0o600); err != nil {
		t.Fatalf("write the harness: %v", err)
	}

	output, err := exec.Command("node", path).CombinedOutput()
	if err != nil {
		t.Fatalf("running the generated script failed: %v\n%s", err, output)
	}

	var made []call
	if err := json.Unmarshal([]byte(lastLine(string(output))), &made); err != nil {
		t.Fatalf("read what the script did: %v\n%s", err, output)
	}

	return made
}

// lastLine is where the harness prints its result, after anything the script
// itself may have written.
func lastLine(output string) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	return lines[len(lines)-1]
}

// A secret reaches the request without being written into the script.
//
// This is what makes a scenario able to authenticate at all: the script is
// stored in the clear and read by anybody who can see the project, so the value
// has to arrive from the environment at run time.
func TestASecretIsReadFromTheEnvironmentAndNeverWrittenDown(t *testing.T) {
	node(t)

	graph := domain.ScenarioGraph{
		Start: "token",
		Nodes: []domain.Node{
			{ID: "token", Kind: domain.NodeSetVariable, Variable: "token", FromEnv: "API_TOKEN"},
			{
				ID: "call", Kind: domain.NodeHTTPRequest, Method: "GET", Path: "/orders",
				Headers: map[string]string{"Authorization": "Bearer {{token}}"},
			},
			{ID: "done", Kind: domain.NodeEnd},
		},
		Edges: []domain.Edge{
			{From: "token", To: "call"},
			{From: "call", To: "done"},
		},
	}

	if err := graph.Validate(); err != nil {
		t.Fatalf("the graph was refused: %v", err)
	}

	script, err := k6.GenerateScript(graph)
	if err != nil {
		t.Fatalf("GenerateScript: %v", err)
	}

	// The key is in the script. The value is not, and cannot be: the generator
	// never sees it.
	if !strings.Contains(script, `__ENV["API_TOKEN"]`) {
		t.Error("the script does not read the secret from the environment")
	}
	if strings.Contains(script, "s3cr3t-value") {
		t.Error("the value of the secret was written into the script")
	}

	calls := runScript(t, script)
	if len(calls) != 1 {
		t.Fatalf("the script made %d request(s), wanted 1", len(calls))
	}
	if calls[0].Headers["Authorization"] != "Bearer s3cr3t-value" {
		t.Errorf("the header arrived as %q, wanted the token in it", calls[0].Headers["Authorization"])
	}
}

// A single brace is ordinary text, because a JSON body is full of them. Only
// {{ }} reads a variable, and the most common body there is has to survive.
func TestASingleBraceInABodyIsText(t *testing.T) {
	node(t)

	graph := oneNode(domain.Node{
		ID: "call", Kind: domain.NodeHTTPRequest, Method: "POST", Path: "/things",
		Body: `{"name":"thing","nested":{"deep":true}}`,
	})

	script, err := k6.GenerateScript(graph)
	if err != nil {
		t.Fatalf("GenerateScript: %v", err)
	}

	calls := runScript(t, script)
	if calls[0].Body != `{"name":"thing","nested":{"deep":true}}` {
		t.Errorf("the body arrived as %q", calls[0].Body)
	}
}

// A variable in a body is substituted, which is how a scenario sends an
// identifier it read from an earlier response.
func TestAVariableInABodyIsSubstituted(t *testing.T) {
	node(t)

	graph := domain.ScenarioGraph{
		Start: "set",
		Nodes: []domain.Node{
			{ID: "set", Kind: domain.NodeSetVariable, Variable: "who", Value: "alice"},
			{
				ID: "call", Kind: domain.NodeHTTPRequest, Method: "POST", Path: "/things",
				Body: `{"user":"{{who}}"}`,
			},
			{ID: "done", Kind: domain.NodeEnd},
		},
		Edges: []domain.Edge{{From: "set", To: "call"}, {From: "call", To: "done"}},
	}

	script, err := k6.GenerateScript(graph)
	if err != nil {
		t.Fatalf("GenerateScript: %v", err)
	}

	calls := runScript(t, script)
	if calls[0].Body != `{"user":"alice"}` {
		t.Errorf("the body arrived as %q, wanted the variable substituted", calls[0].Body)
	}
}

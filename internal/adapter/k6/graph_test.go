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

// reply is what the stubbed server answers: the same for every request.
type reply struct {
	Status  int               `json:"status"`
	Body    any               `json:"body"`
	Headers map[string]string `json:"headers"`
	// DurationMS is how long the stubbed server "took", as k6 reports it.
	DurationMS float64 `json:"duration"`
}

// verdict is one check() the script made, and whether it held.
type verdict struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
}

// outcome is what running the script did: the requests it made, and what stopped
// it early, if anything did.
type outcome struct {
	Calls   []call    `json:"calls"`
	Failure string    `json:"failure"`
	Sleeps  []float64 `json:"sleeps"`
	Checks  []verdict `json:"checks"`
}

// runScript executes the generated script with k6 replaced by stubs, and
// returns the requests it made. A script that stops itself with fail() is a test
// failure here: the ones that mean to are run with runScriptAgainst.
//
// The imports are stripped and the module's own default function is called, so
// what runs is exactly the body this generator wrote. If a value escaped into
// code, it runs here — and either it throws, or it shows up somewhere the test
// does not expect it.
func runScript(t *testing.T, script string) []call {
	t.Helper()

	result := runScriptAgainst(t, script, reply{Status: 200})
	if result.Failure != "" {
		t.Fatalf("the script stopped itself: %s", result.Failure)
	}
	return result.Calls
}

// runScriptAgainst runs the script with the stub answering as given, and says what
// it did and why it stopped. fail() in the stub throws, as it does in k6, which
// ends the iteration.
func runScriptAgainst(t *testing.T, script string, answer reply) outcome {
	t.Helper()

	var body strings.Builder
	for _, line := range strings.Split(script, "\n") {
		if strings.HasPrefix(line, "import ") {
			continue
		}
		body.WriteString(strings.Replace(line, "export default function ()", "const scenario = function ()", 1))
		body.WriteString("\n")
	}

	encoded, err := json.Marshal(answer)
	if err != nil {
		t.Fatalf("encode the stubbed answer: %v", err)
	}

	harness := `const calls = [];
const __ENV = { BASE_URL: "http://target.test", API_TOKEN: "s3cr3t-value" };
const answer = ` + string(encoded) + `;
const http = {
  request(method, url, requestBody, params) {
    calls.push({
      method,
      url,
      body: requestBody === null ? "" : String(requestBody),
      headers: (params && params.headers) || {},
    });
    // json() throws on a body that is not JSON, as k6's does.
    return {
      status: answer.status,
      timings: { duration: answer.duration || 0 },
      headers: answer.headers || {},
      json: (selector) => {
        if (answer.body === undefined || answer.body === null) throw new Error("not JSON");
        return String(selector).split(".").reduce((at, key) => (at == null ? undefined : at[key]), answer.body);
      },
    };
  },
};
const checks = [];
const check = (value, conditions) => {
  for (const name of Object.keys(conditions)) checks.push({ name, passed: Boolean(conditions[name](value)) });
  return true;
};
const fail = (message) => { throw new Error("fail() was called: " + message); };
const sleeps = [];
const sleep = (seconds) => { sleeps.push(seconds); };
// Half way through any range, so that a random wait is a number a test can name.
Math.random = () => 0.5;

` + body.String() + `
let failure = "";
try { scenario(); } catch (error) { failure = String(error.message); }
console.log(JSON.stringify({ calls, failure, sleeps, checks }));
`

	path := filepath.Join(t.TempDir(), "harness.mjs")
	if err := os.WriteFile(path, []byte(harness), 0o600); err != nil {
		t.Fatalf("write the harness: %v", err)
	}

	output, err := exec.Command("node", path).CombinedOutput()
	if err != nil {
		t.Fatalf("running the generated script failed: %v\n%s", err, output)
	}

	var result outcome
	if err := json.Unmarshal([]byte(lastLine(string(output))), &result); err != nil {
		t.Fatalf("read what the script did: %v\n%s", err, output)
	}
	return result
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

// The query string is built from the parameters in the order given, each name
// and value encoded, and it leaves the request's name tag alone.
func TestQueryParametersAreEncodedInOrderAndStayOutOfTheTag(t *testing.T) {
	node(t)

	graph := domain.ScenarioGraph{
		Start: "set",
		Nodes: []domain.Node{
			{ID: "set", Kind: domain.NodeSetVariable, Variable: "who", Value: "a b&c"},
			{
				ID: "call", Kind: domain.NodeHTTPRequest, Method: "GET", Path: "/search",
				Query: []domain.QueryParam{
					{Name: "page", Value: "2"},
					{Name: "q name", Value: "x=y/z"},
					{Name: "user", Value: "{{who}}"},
				},
			},
			{ID: "done", Kind: domain.NodeEnd},
		},
		Edges: []domain.Edge{{From: "set", To: "call"}, {From: "call", To: "done"}},
	}

	script, err := k6.GenerateScript(graph)
	if err != nil {
		t.Fatalf("GenerateScript: %v", err)
	}
	mustParse(t, script)

	calls := runScript(t, script)
	wanted := "http://target.test/search?page=2&q%20name=x%3Dy%2Fz&user=a%20b%26c"
	if calls[0].URL != wanted {
		t.Errorf("the address was %q, wanted %q", calls[0].URL, wanted)
	}
	if !strings.Contains(script, `tags: { name: "GET /search" }`) {
		t.Errorf("the name tag should be the path alone:\n%s", script)
	}
}

// The group and the label come from somebody else's file. They are written as
// one comment line, so no line break in them can start code.
func TestAHostileLabelCannotLeaveItsCommentLine(t *testing.T) {
	node(t)

	graph := oneNode(domain.Node{
		ID: "call", Kind: domain.NodeHTTPRequest, Method: "GET", Path: "/items",
		Group: "Folder\nfail('group escaped')",
		Label: "Name fail('label escaped')\r\nfail('crlf escaped')",
	})

	script, err := k6.GenerateScript(graph)
	if err != nil {
		t.Fatalf("GenerateScript: %v", err)
	}
	mustParse(t, script)

	// Running it would call fail() and throw if any of it had become code.
	calls := runScript(t, script)
	if len(calls) != 1 {
		t.Fatalf("%d requests, wanted 1", len(calls))
	}

	commentLines := 0
	for _, line := range strings.Split(script, "\n") {
		if strings.Contains(line, "escaped") {
			commentLines++
			if !strings.HasPrefix(strings.TrimSpace(line), "//") {
				t.Errorf("text from a label is outside a comment: %q", line)
			}
		}
	}
	if commentLines != 1 {
		t.Errorf("the label was written on %d lines, wanted 1", commentLines)
	}
}

func TestAGraphWithQueryAndLabelsIsStillDeterministic(t *testing.T) {
	graph := oneNode(domain.Node{
		ID: "call", Kind: domain.NodeHTTPRequest, Method: "GET", Path: "/items",
		Group: "Orders", Label: "List",
		Query: []domain.QueryParam{{Name: "b", Value: "1"}, {Name: "a", Value: "2"}},
	})

	first, err := k6.GenerateScript(graph)
	if err != nil {
		t.Fatalf("GenerateScript: %v", err)
	}
	for attempt := 0; attempt < 20; attempt++ {
		again, _ := k6.GenerateScript(graph)
		if again != first {
			t.Fatalf("attempt %d differs", attempt)
		}
	}
	// Given order, not sorted order: a collection's order means something.
	if strings.Index(first, `"b"`) > strings.Index(first, `"a"`) {
		t.Errorf("the query parameters were reordered:\n%s", first)
	}
}

// extractThenCall reads one value from a response and uses it in the next request,
// which is what an extraction is for.
func extractThenCall(extract domain.Node) domain.ScenarioGraph {
	extract.ID, extract.Kind = "take", domain.NodeExtractVariable
	return domain.ScenarioGraph{
		Start: "first",
		Nodes: []domain.Node{
			{ID: "first", Kind: domain.NodeHTTPRequest, Method: "POST", Path: "/login"},
			extract,
			{
				ID: "second", Kind: domain.NodeHTTPRequest, Method: "GET", Path: "/me",
				Headers: map[string]string{"X-Seen": "{{seen}}"},
			},
			{ID: "done", Kind: domain.NodeEnd},
		},
		Edges: []domain.Edge{
			{From: "first", To: "take"}, {From: "take", To: "second"}, {From: "second", To: "done"},
		},
	}
}

func generate(t *testing.T, graph domain.ScenarioGraph) string {
	t.Helper()
	if err := graph.Validate(); err != nil {
		t.Fatalf("the graph was refused: %v", err)
	}
	script, err := k6.GenerateScript(graph)
	if err != nil {
		t.Fatalf("GenerateScript: %v", err)
	}
	return script
}

// The value read from the response is the one the next request carries.
func TestAnExtractedValueReachesTheNextRequest(t *testing.T) {
	node(t)

	cases := map[string]struct {
		extract domain.Node
		answer  reply
		wanted  string
	}{
		"from the body": {
			domain.Node{Variable: "seen", From: "data.session.token"},
			reply{Status: 200, Body: map[string]any{"data": map[string]any{"session": map[string]any{"token": "abc"}}}},
			"abc",
		},
		"from the body, written as JSONPath": {
			domain.Node{Variable: "seen", From: "$.data.token"},
			reply{Status: 200, Body: map[string]any{"data": map[string]any{"token": "xyz"}}},
			"xyz",
		},
		"a number is kept, zero included": {
			domain.Node{Variable: "seen", From: "count"},
			reply{Status: 200, Body: map[string]any{"count": 0}},
			"0",
		},
		"from a header, whatever its case": {
			domain.Node{Variable: "seen", Source: domain.ExtractFromHeader, From: "x-request-id"},
			reply{Status: 200, Headers: map[string]string{"X-Request-Id": "req-7"}},
			"req-7",
		},
		"from the status": {
			domain.Node{Variable: "seen", Source: domain.ExtractFromStatus},
			reply{Status: 201},
			"201",
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			result := runScriptAgainst(t, generate(t, extractThenCall(c.extract)), c.answer)

			if result.Failure != "" {
				t.Fatalf("the script stopped: %s", result.Failure)
			}
			if len(result.Calls) != 2 {
				t.Fatalf("%d request(s) were made, wanted 2", len(result.Calls))
			}
			if got := result.Calls[1].Headers["X-Seen"]; got != c.wanted {
				t.Errorf("the next request carried %q, wanted %q", got, c.wanted)
			}
		})
	}
}

// When the value is not there the iteration stops, with the variable's name, and
// nothing is sent with an empty value in its place.
func TestAFailedExtractionStopsTheIteration(t *testing.T) {
	node(t)

	cases := map[string]struct {
		extract domain.Node
		answer  reply
	}{
		"a path the body does not have": {
			domain.Node{Variable: "seen", From: "data.token"},
			reply{Status: 200, Body: map[string]any{"data": map[string]any{}}},
		},
		"a body that is not JSON": {
			domain.Node{Variable: "seen", From: "data.token"},
			reply{Status: 200},
		},
		"a null value": {
			domain.Node{Variable: "seen", From: "token"},
			reply{Status: 200, Body: map[string]any{"token": nil}},
		},
		"an empty value": {
			domain.Node{Variable: "seen", From: "token"},
			reply{Status: 200, Body: map[string]any{"token": ""}},
		},
		"a header the response does not have": {
			domain.Node{Variable: "seen", Source: domain.ExtractFromHeader, From: "X-Missing"},
			reply{Status: 200, Headers: map[string]string{"X-Other": "1"}},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			result := runScriptAgainst(t, generate(t, extractThenCall(c.extract)), c.answer)

			if !strings.Contains(result.Failure, "fail() was called") {
				t.Fatalf("the script did not stop itself: %q", result.Failure)
			}
			if !strings.Contains(result.Failure, `"seen"`) {
				t.Errorf("the failure does not name the variable: %q", result.Failure)
			}
			if len(result.Calls) != 1 {
				t.Errorf("%d request(s) were made, wanted only the first: the next must not go out empty", len(result.Calls))
			}
		})
	}
}

// What the response held is never in the failure: it may be a token.
func TestAFailedExtractionDoesNotRepeatTheResponse(t *testing.T) {
	node(t)

	graph := extractThenCall(domain.Node{Variable: "seen", From: "missing"})
	result := runScriptAgainst(t, generate(t, graph), reply{
		Status: 200, Body: map[string]any{"token": "do-not-repeat-me"}, Headers: map[string]string{"Set-Cookie": "sid=do-not-repeat-me"},
	})

	if result.Failure == "" || strings.Contains(result.Failure, "do-not-repeat-me") {
		t.Errorf("the failure is %q", result.Failure)
	}
}

// A path typed with quotes or template syntax stays text, like everything else.
func TestAnExtractionPathIsNeverCode(t *testing.T) {
	node(t)

	hostile := `a"]; throw new Error("pwned"); //${1+1}` + "`"
	script := generate(t, extractThenCall(domain.Node{Variable: "seen", From: hostile}))

	// The path is text in the message — it is how the person finds the node — and
	// the failure is the generator's own fail(), not an error thrown by the path.
	result := runScriptAgainst(t, script, reply{Status: 200, Body: map[string]any{"a": "x"}})
	if !strings.HasPrefix(result.Failure, "fail() was called: could not extract") {
		t.Errorf("the path was not kept as text: %q", result.Failure)
	}
	if len(result.Calls) != 1 {
		t.Errorf("%d request(s) were made, wanted 1", len(result.Calls))
	}
}

// A pause waits the time it says, or a time picked between its two bounds.
func TestAPauseWaitsAsLongAsItSays(t *testing.T) {
	node(t)

	cases := map[string]struct {
		pause  domain.Node
		wanted float64
	}{
		"a fixed time":             {domain.Node{Seconds: 2}, 2},
		"a fraction of a second":   {domain.Node{Seconds: 0.25}, 0.25},
		"a range, half way":        {domain.Node{Seconds: 1, SecondsMax: 5}, 3},
		"a range of a single time": {domain.Node{Seconds: 4, SecondsMax: 4}, 4},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			c.pause.ID, c.pause.Kind = "wait", domain.NodeThinkTime
			result := runScriptAgainst(t, generate(t, oneNode(c.pause)), reply{Status: 200})

			if len(result.Sleeps) != 1 || result.Sleeps[0] != c.wanted {
				t.Errorf("it waited %v, wanted one wait of %v", result.Sleeps, c.wanted)
			}
		})
	}
}

// listLoop takes each element of a list the first request returned, and uses it
// in a request of its own.
func listLoop(maximum int) domain.ScenarioGraph {
	return domain.ScenarioGraph{
		Start: "first",
		Nodes: []domain.Node{
			{ID: "first", Kind: domain.NodeHTTPRequest, Method: "GET", Path: "/items"},
			{ID: "take", Kind: domain.NodeExtractVariable, Variable: "items", From: "ids"},
			{ID: "each", Kind: domain.NodeLoop, Iterations: maximum, Over: "items", Item: "item"},
			{ID: "one", Kind: domain.NodeHTTPRequest, Method: "GET", Path: "/items/{item}"},
			{ID: "done", Kind: domain.NodeEnd},
		},
		Edges: []domain.Edge{
			{From: "first", To: "take"},
			{From: "take", To: "each"},
			{From: "each", To: "one", When: "body"},
			{From: "each", To: "done", When: "after"},
			{From: "one", To: "each"},
		},
	}
}

// Walking a list takes each element in turn, and never more of them than the
// maximum the loop was given.
func TestALoopOverAListTakesEachElementUpToItsMaximum(t *testing.T) {
	node(t)

	urls := func(result outcome) []string {
		var out []string
		for _, c := range result.Calls[1:] {
			out = append(out, strings.TrimPrefix(c.URL, "http://target.test"))
		}
		return out
	}

	t.Run("a list shorter than the maximum", func(t *testing.T) {
		result := runScriptAgainst(t, generate(t, listLoop(10)), reply{
			Status: 200, Body: map[string]any{"ids": []any{"a", "b", "c"}},
		})
		if result.Failure != "" {
			t.Fatalf("the script stopped: %s", result.Failure)
		}
		if got := strings.Join(urls(result), " "); got != "/items/a /items/b /items/c" {
			t.Errorf("the loop called %q", got)
		}
	})

	t.Run("a list longer than the maximum", func(t *testing.T) {
		result := runScriptAgainst(t, generate(t, listLoop(2)), reply{
			Status: 200, Body: map[string]any{"ids": []any{"a", "b", "c", "d"}},
		})
		if got := strings.Join(urls(result), " "); got != "/items/a /items/b" {
			t.Errorf("the loop called %q, wanted only the first two", got)
		}
	})

	t.Run("an empty list", func(t *testing.T) {
		result := runScriptAgainst(t, generate(t, listLoop(5)), reply{
			Status: 200, Body: map[string]any{"ids": []any{}},
		})
		if result.Failure != "" || len(result.Calls) != 1 {
			t.Errorf("an empty list should walk nothing: failure %q, %d call(s)", result.Failure, len(result.Calls))
		}
	})

	t.Run("an element with a slash is encoded, not followed", func(t *testing.T) {
		result := runScriptAgainst(t, generate(t, listLoop(5)), reply{
			Status: 200, Body: map[string]any{"ids": []any{"a/b?x=1"}},
		})
		if got := strings.Join(urls(result), " "); got != "/items/a%2Fb%3Fx%3D1" {
			t.Errorf("the element changed the address: %q", got)
		}
	})
}

// Something that is not a list stops the iteration with the name of the variable,
// and never with what it held.
func TestALoopOverSomethingThatIsNotAListStops(t *testing.T) {
	node(t)

	result := runScriptAgainst(t, generate(t, listLoop(5)), reply{
		Status: 200, Body: map[string]any{"ids": "do-not-repeat-me"},
	})

	if !strings.Contains(result.Failure, "fail() was called") || !strings.Contains(result.Failure, `"items"`) {
		t.Errorf("the failure is %q", result.Failure)
	}
	if strings.Contains(result.Failure, "do-not-repeat-me") {
		t.Error("the failure repeats what the variable held")
	}
	if len(result.Calls) != 1 {
		t.Errorf("%d request(s) were made, wanted only the first", len(result.Calls))
	}
}

// assertAfterCall checks something about the response of one request, and then
// makes another: whether that second request goes out says whether the check
// stopped the iteration.
func assertAfterCall(expression string, stop bool) domain.ScenarioGraph {
	return domain.ScenarioGraph{
		Start: "first",
		Nodes: []domain.Node{
			{ID: "first", Kind: domain.NodeHTTPRequest, Method: "GET", Path: "/a"},
			{ID: "check", Kind: domain.NodeAssertion, Expression: expression, Critical: stop},
			{ID: "second", Kind: domain.NodeHTTPRequest, Method: "GET", Path: "/b"},
			{ID: "done", Kind: domain.NodeEnd},
		},
		Edges: []domain.Edge{{From: "first", To: "check"}, {From: "check", To: "second"}, {From: "second", To: "done"}},
	}
}

// An assertion can speak of the response itself — its status and how long it took
// — and a failed one either counts or stops, as it was asked to.
func TestAnAssertionChecksTheResponseAndCountsOrStops(t *testing.T) {
	node(t)

	cases := map[string]struct {
		expression string
		stop       bool
		answer     reply
		holds      bool
		calls      int
	}{
		"status holds":               {"response_status == 200", false, reply{Status: 200}, true, 2},
		"status fails, counted only": {"response_status == 200", false, reply{Status: 500}, false, 2},
		"status fails, stops":        {"response_status == 200", true, reply{Status: 500}, false, 1},
		"fast enough":                {"response_time_ms < 500", true, reply{Status: 200, DurationMS: 120}, true, 2},
		"too slow, counted only":     {"response_time_ms < 500", false, reply{Status: 200, DurationMS: 900}, false, 2},
		"too slow, stops":            {"response_time_ms < 500", true, reply{Status: 200, DurationMS: 900}, false, 1},
		"both, one of them failing":  {"response_status == 200 && response_time_ms < 500", true, reply{Status: 200, DurationMS: 900}, false, 1},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			result := runScriptAgainst(t, generate(t, assertAfterCall(c.expression, c.stop)), c.answer)

			if len(result.Checks) != 1 || result.Checks[0].Passed != c.holds {
				t.Errorf("the check was %+v, wanted passed=%v", result.Checks, c.holds)
			}
			if len(result.Calls) != c.calls {
				t.Errorf("%d request(s) went out, wanted %d", len(result.Calls), c.calls)
			}
			if !c.stop && result.Failure != "" {
				t.Errorf("a check that only counts stopped the iteration: %s", result.Failure)
			}
			if c.stop && !c.holds && !strings.Contains(result.Failure, "fail() was called") {
				t.Errorf("an assertion that stops did not: %q", result.Failure)
			}
		})
	}
}

// With no response yet, a comparison on it is simply false: nothing is read from
// a request that was never made.
func TestTheResponseNamesReadNothingBeforeAnyRequest(t *testing.T) {
	node(t)

	graph := oneNode(domain.Node{ID: "check", Kind: domain.NodeAssertion, Expression: "response_time_ms < 500"})
	result := runScriptAgainst(t, generate(t, graph), reply{Status: 200})

	if len(result.Checks) != 1 || result.Checks[0].Passed {
		t.Errorf("a time that was never measured passed a bound: %+v", result.Checks)
	}
}

// A condition sends the run down one way or the other, by what it holds.
func TestAConditionTakesOneWayOrTheOther(t *testing.T) {
	node(t)

	graph := domain.ScenarioGraph{
		Start: "first",
		Nodes: []domain.Node{
			{ID: "first", Kind: domain.NodeHTTPRequest, Method: "GET", Path: "/a"},
			{ID: "branch", Kind: domain.NodeCondition, Expression: "response_status == 200 && response_time_ms < 500"},
			{ID: "yes", Kind: domain.NodeHTTPRequest, Method: "GET", Path: "/yes"},
			{ID: "no", Kind: domain.NodeHTTPRequest, Method: "GET", Path: "/no"},
			{ID: "end1", Kind: domain.NodeEnd},
			{ID: "end2", Kind: domain.NodeEnd},
		},
		Edges: []domain.Edge{
			{From: "first", To: "branch"},
			{From: "branch", To: "yes", When: "true"}, {From: "branch", To: "no", When: "false"},
			{From: "yes", To: "end1"}, {From: "no", To: "end2"},
		},
	}
	script := generate(t, graph)

	last := func(answer reply) string {
		calls := runScriptAgainst(t, script, answer).Calls
		return strings.TrimPrefix(calls[len(calls)-1].URL, "http://target.test")
	}

	if got := last(reply{Status: 200, DurationMS: 100}); got != "/yes" {
		t.Errorf("a fast 200 went to %q", got)
	}
	if got := last(reply{Status: 200, DurationMS: 900}); got != "/no" {
		t.Errorf("a slow 200 went to %q", got)
	}
	if got := last(reply{Status: 500, DurationMS: 100}); got != "/no" {
		t.Errorf("a fast 500 went to %q", got)
	}
}

// A node id may hold a hyphen, which a JavaScript name may not: the names the
// generator makes from it have to cope, or the script would not even parse.
func TestANodeIdWithAHyphenStillMakesAScript(t *testing.T) {
	node(t)

	graph := domain.ScenarioGraph{
		Start: "get-list",
		Nodes: []domain.Node{
			{ID: "get-list", Kind: domain.NodeHTTPRequest, Method: "GET", Path: "/items"},
			{ID: "take-ids", Kind: domain.NodeExtractVariable, Variable: "items", From: "ids"},
			{ID: "each-item", Kind: domain.NodeLoop, Iterations: 3, Over: "items", Item: "item"},
			{ID: "check-it", Kind: domain.NodeAssertion, Expression: "response_status == 200"},
			{ID: "all-done", Kind: domain.NodeEnd},
		},
		Edges: []domain.Edge{
			{From: "get-list", To: "take-ids"}, {From: "take-ids", To: "each-item"},
			{From: "each-item", To: "check-it", When: "body"}, {From: "each-item", To: "all-done", When: "after"},
			{From: "check-it", To: "each-item"},
		},
	}

	result := runScriptAgainst(t, generate(t, graph), reply{Status: 200, Body: map[string]any{"ids": []any{1, 2}}})
	if result.Failure != "" || len(result.Checks) != 2 {
		t.Errorf("the script did not run as drawn: failure %q, %d check(s)", result.Failure, len(result.Checks))
	}
}

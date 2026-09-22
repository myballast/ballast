package k6_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/youssefmahersi/ballast/internal/adapter/k6"
	"github.com/youssefmahersi/ballast/internal/domain"
)

func readFixture(t *testing.T) []byte {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("testdata", "summary.json"))
	if err != nil {
		t.Fatalf("read the fixture: %v", err)
	}
	return raw
}

func parseFixture(t *testing.T, raw []byte) domain.RunResults {
	t.Helper()

	results, err := k6.NewSummaryParser().Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return results
}

// render is how the parser's output is judged: not field by field, but as the
// bytes that would be written to storage or to a log. Nothing sensitive may
// survive the round trip.
func render(t *testing.T, results domain.RunResults) string {
	t.Helper()

	raw, err := json.Marshal(results)
	if err != nil {
		t.Fatalf("marshal the results: %v", err)
	}
	return string(raw)
}

// The reason this parser exists. k6 puts whatever setup() returned into
// setup_data, and a scenario that logs in returns a valid token.
func TestParseDropsTheAuthenticationToken(t *testing.T) {
	raw := readFixture(t)

	var source struct {
		SetupData struct {
			Token string `json:"token"`
		} `json:"setup_data"`
	}
	if err := json.Unmarshal(raw, &source); err != nil {
		t.Fatalf("read setup_data from the fixture: %v", err)
	}
	if source.SetupData.Token == "" {
		t.Fatal("the fixture carries no token, so this test proves nothing")
	}

	got := render(t, parseFixture(t, raw))

	if strings.Contains(got, source.SetupData.Token) {
		t.Error("the authentication token survived into the parsed results")
	}
	if strings.Contains(got, "setup_data") {
		t.Error("the setup_data key survived into the parsed results")
	}
	// The signature alone is enough to identify the token, and a partial leak
	// is still a leak.
	for _, segment := range strings.Split(source.SetupData.Token, ".") {
		if len(segment) > 8 && strings.Contains(got, segment) {
			t.Errorf("a fragment of the token survived: %s…", segment[:8])
		}
	}
}

// Three objects of a k6 summary are dictionaries keyed by data — metric names,
// statistic names, threshold expressions — rather than records with fields.
// Adding a key to one of those does not add an unknown field, it invents a
// malformed metric, which the parser is right to refuse outright;
// TestParseRejectsMalformedInput covers that case on its own.
//
// This is knowledge of the k6 summary format, not of the parser.
func isDictionary(path string) bool {
	return path == "metrics" ||
		strings.HasSuffix(path, "/values") ||
		strings.HasSuffix(path, "/thresholds")
}

// Poisons every record of the fixture rather than adding one unknown field in
// one place. The allowlist has to hold everywhere: at the root, inside state,
// inside each metric, inside each group and inside each check.
func poison(value any, canary, path string, records *int) any {
	switch typed := value.(type) {
	case map[string]any:
		poisoned := make(map[string]any, len(typed)+1)
		for key, sub := range typed {
			child := key
			if path != "" {
				child = path + "/" + key
			}
			poisoned[key] = poison(sub, canary, child, records)
		}

		if !isDictionary(path) {
			*records++
			poisoned["ballast_unknown_field"] = canary
		}
		return poisoned

	case []any:
		poisoned := make([]any, len(typed))
		for i, sub := range typed {
			poisoned[i] = poison(sub, canary, path, records)
		}
		return poisoned

	default:
		return value
	}
}

func TestParseDropsUnknownFieldsEverywhere(t *testing.T) {
	const canary = "canary-8f3a1c-must-never-be-read"

	var document any
	if err := json.Unmarshal(readFixture(t), &document); err != nil {
		t.Fatalf("decode the fixture: %v", err)
	}

	records := 0
	poisoned, err := json.Marshal(poison(document, canary, "", &records))
	if err != nil {
		t.Fatalf("re-encode the poisoned fixture: %v", err)
	}
	if records < 30 {
		t.Fatalf("only %d records were poisoned; the fixture is not what this test assumes", records)
	}
	t.Logf("%d records poisoned", records)

	got := render(t, parseFixture(t, poisoned))

	if strings.Contains(got, canary) {
		t.Error("an unknown field was carried into the parsed results")
	}
	if strings.Contains(got, "ballast_unknown_field") {
		t.Error("an unknown key was carried into the parsed results")
	}
}

// The verdict belongs to k6. Ballast reads thresholds.<expression>.ok and never
// recomputes it from the values.
func TestParseReadsThresholdVerdicts(t *testing.T) {
	results := parseFixture(t, readFixture(t))

	want := []domain.ThresholdResult{
		{Metric: "business_errors", Expression: "rate<0.05", Passed: true},
		{Metric: "http_req_duration", Expression: "p(95)<1000", Passed: false},
		{Metric: "http_req_failed", Expression: "rate<0.01", Passed: true},
		{Metric: "login_duration", Expression: "p(95)<1000", Passed: false},
	}

	if len(results.Thresholds) != len(want) {
		t.Fatalf("got %d thresholds, want %d: %+v", len(results.Thresholds), len(want), results.Thresholds)
	}
	for i, w := range want {
		if results.Thresholds[i] != w {
			t.Errorf("threshold %d: got %+v, want %+v", i, results.Thresholds[i], w)
		}
	}
}

func findMetric(results domain.RunResults, metric, tag, stat string) (domain.RunMetric, bool) {
	for _, m := range results.Metrics {
		if m.Metric == metric && m.Tag == tag && m.Stat == stat {
			return m, true
		}
	}
	return domain.RunMetric{}, false
}

func TestParseReadsMetrics(t *testing.T) {
	results := parseFixture(t, readFixture(t))

	cases := []struct {
		metric, tag, stat string
		want              float64
	}{
		{"http_req_duration", "", "p(95)", 5770.7336},
		{"http_req_duration", "", "avg", 2222.5338805460747},
		{"http_req_duration", "", "min", 48.808},
		{"http_reqs", "", "count", 293},
		{"http_reqs", "", "rate", 2.3748754902628524},
		{"vus_max", "", "value", 10},
		// A submetric: k6 writes the tag selector into the metric key, and it
		// is what makes one threshold per endpoint possible.
		{"http_req_duration", "expected_response:true", "p(95)", 5770.7336},
	}

	for _, c := range cases {
		got, ok := findMetric(results, c.metric, c.tag, c.stat)
		if !ok {
			t.Errorf("%s{%s}.%s is missing from the results", c.metric, c.tag, c.stat)
			continue
		}
		if got.Value != c.want {
			t.Errorf("%s{%s}.%s = %v, want %v", c.metric, c.tag, c.stat, got.Value, c.want)
		}
	}

	// The tag selector must not stay glued to the metric name, or a query for
	// http_req_duration would miss every submetric.
	for _, m := range results.Metrics {
		if strings.ContainsAny(m.Metric, "{}") {
			t.Errorf("metric name %q still carries its tag selector", m.Metric)
		}
	}
}

func TestParseReadsChecks(t *testing.T) {
	results := parseFixture(t, readFixture(t))

	want := []domain.CheckResult{
		{Group: "login", Name: "login returns a token", Passes: 146, Fails: 0},
		{Group: "login", Name: "login status is 200", Passes: 146, Fails: 0},
		{Group: "read Configuration", Name: "config status is 200", Passes: 146, Fails: 0},
	}

	if len(results.Checks) != len(want) {
		t.Fatalf("got %d checks, want %d: %+v", len(results.Checks), len(want), results.Checks)
	}
	for i, w := range want {
		if results.Checks[i] != w {
			t.Errorf("check %d: got %+v, want %+v", i, results.Checks[i], w)
		}
	}
}

func TestParseReadsRunDuration(t *testing.T) {
	results := parseFixture(t, readFixture(t))

	if results.DurationMs != 123374.889 {
		t.Errorf("DurationMs = %v, want 123374.889", results.DurationMs)
	}
}

// Two identical summaries must produce two identical results. k6 hands the
// metrics over in a map, and Go iterates maps in a random order.
func TestParseIsDeterministic(t *testing.T) {
	raw := readFixture(t)

	first := render(t, parseFixture(t, raw))
	for i := range 20 {
		if got := render(t, parseFixture(t, raw)); got != first {
			t.Fatalf("run %d produced different results from run 0", i+1)
		}
	}
}

// A summary is written by a container running a user's script. It is not
// trusted input.
func TestParseRejectsMalformedInput(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"empty", ""},
		{"not json", "this is not json"},
		{"truncated", `{"metrics": {"http_reqs": {"values": {"count": 1`},
		{"an array", `[1, 2, 3]`},
		{"a string", `"summary"`},
		{"null", `null`},
		{"metric values of the wrong type", `{"metrics": {"http_reqs": {"values": {"count": "many"}}}}`},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := k6.NewSummaryParser().Parse([]byte(c.raw)); err == nil {
				t.Error("Parse accepted a summary it should have refused")
			}
		})
	}
}

// An empty but well-formed summary is not an error: a run that produced no
// metric still has to be recorded rather than crash the worker.
func TestParseAcceptsAnEmptySummary(t *testing.T) {
	results, err := k6.NewSummaryParser().Parse([]byte(`{}`))
	if err != nil {
		t.Fatalf("Parse refused an empty summary: %v", err)
	}

	if len(results.Metrics) != 0 || len(results.Thresholds) != 0 || len(results.Checks) != 0 {
		t.Errorf("an empty summary produced %+v", results)
	}
}

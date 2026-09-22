package k6

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// ErrInvalidSummary reports a summary that is not a k6 summary. The file is
// written by a container running a user's script, so it is not trusted input.
var ErrInvalidSummary = errors.New("k6: invalid summary")

// The types below are the allowlist, and they are the whole mechanism.
//
// encoding/json ignores anything the destination does not declare, so a field
// that is absent here is never read — not stripped afterwards, never read. That
// matters most for setup_data, which holds whatever the script's setup()
// returned: for any scenario that authenticates, a valid token. It is not
// named anywhere in this file, and that is the point. A field k6 adds in a
// future version is excluded by the same silence.
//
// The one opening left is a metric's values, whose keys vary with the metric
// type — avg, p(95), count, rate, value. They cannot be listed, so they are
// read as float64: anything that is not a number is refused rather than
// carried through.
type summary struct {
	Metrics   map[string]summaryMetric `json:"metrics"`
	RootGroup *summaryGroup            `json:"root_group"`
	State     summaryState             `json:"state"`
}

type summaryState struct {
	TestRunDurationMs float64 `json:"testRunDurationMs"`
}

type summaryMetric struct {
	Values     map[string]float64          `json:"values"`
	Thresholds map[string]summaryThreshold `json:"thresholds"`
}

type summaryThreshold struct {
	OK bool `json:"ok"`
}

type summaryGroup struct {
	Name   string          `json:"name"`
	Checks []summaryCheck  `json:"checks"`
	Groups []*summaryGroup `json:"groups"`
}

type summaryCheck struct {
	Name   string `json:"name"`
	Passes int    `json:"passes"`
	Fails  int    `json:"fails"`
}

// SummaryParser turns a k6 summary into the results Ballast keeps.
type SummaryParser struct{}

// NewSummaryParser returns a parser. It holds no state; a single one serves
// every run.
func NewSummaryParser() SummaryParser {
	return SummaryParser{}
}

// Parse reads a k6 summary.
//
// An empty but well-formed summary is not an error: a run that measured nothing
// still has to be recorded rather than take the worker down with it. A summary
// that is not an object, or whose fields are of the wrong type, is refused.
func (SummaryParser) Parse(raw []byte) (domain.RunResults, error) {
	// A pointer, so that a literal null is told apart from an empty object.
	// Decoding null into a struct leaves it untouched and reports no error.
	var doc *summary

	// encoding/json refuses a document nested deeper than its own limit, which
	// is what keeps a hostile summary from exhausting the stack here.
	if err := json.Unmarshal(raw, &doc); err != nil {
		return domain.RunResults{}, fmt.Errorf("%w: %v", ErrInvalidSummary, err)
	}
	if doc == nil {
		return domain.RunResults{}, fmt.Errorf("%w: the summary is null", ErrInvalidSummary)
	}

	return domain.RunResults{
		DurationMs: doc.State.TestRunDurationMs,
		Metrics:    metricsOf(doc.Metrics),
		Thresholds: thresholdsOf(doc.Metrics),
		Checks:     checksOf(doc.RootGroup, ""),
	}, nil
}

// metricsOf flattens the summary's metrics into one row per measured number.
//
// The result is sorted because k6 hands the metrics over in a map and Go walks
// maps in a random order. Without this, two runs of the same test would produce
// two different rows in the database and two different files in storage.
func metricsOf(metrics map[string]summaryMetric) []domain.RunMetric {
	var out []domain.RunMetric

	for key, metric := range metrics {
		name, tag := splitSubmetric(key)

		for stat, value := range metric.Values {
			out = append(out, domain.RunMetric{
				Metric: name,
				Tag:    tag,
				Stat:   stat,
				Value:  value,
			})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Metric != b.Metric {
			return a.Metric < b.Metric
		}
		if a.Tag != b.Tag {
			return a.Tag < b.Tag
		}
		return a.Stat < b.Stat
	})

	return out
}

// thresholdsOf collects k6's verdicts. The metric keeps its tag selector here:
// a threshold is declared against the selector, and that is how it is stored.
func thresholdsOf(metrics map[string]summaryMetric) []domain.ThresholdResult {
	var out []domain.ThresholdResult

	for metric, m := range metrics {
		for expression, verdict := range m.Thresholds {
			out = append(out, domain.ThresholdResult{
				Metric:     metric,
				Expression: expression,
				Passed:     verdict.OK,
			})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Metric != b.Metric {
			return a.Metric < b.Metric
		}
		return a.Expression < b.Expression
	})

	return out
}

// checksOf walks the group tree, which k6 nests as deeply as the script does.
// The root group has no name and contributes nothing to the path.
func checksOf(group *summaryGroup, path string) []domain.CheckResult {
	if group == nil {
		return nil
	}

	if group.Name != "" {
		if path == "" {
			path = group.Name
		} else {
			path += "::" + group.Name
		}
	}

	var out []domain.CheckResult
	for _, check := range group.Checks {
		out = append(out, domain.CheckResult{
			Group:  path,
			Name:   check.Name,
			Passes: check.Passes,
			Fails:  check.Fails,
		})
	}
	for _, sub := range group.Groups {
		out = append(out, checksOf(sub, path)...)
	}

	// Sorting at every level costs nothing at these sizes and leaves the whole
	// slice ordered once the recursion unwinds.
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Group != b.Group {
			return a.Group < b.Group
		}
		return a.Name < b.Name
	})

	return out
}

// splitSubmetric takes the tag selector off a metric key. k6 writes a submetric
// as metric{tag:value}; leaving the selector glued to the name would mean a
// query for http_req_duration misses every submetric of it.
func splitSubmetric(key string) (metric, tag string) {
	open := strings.IndexByte(key, '{')
	if open < 0 || !strings.HasSuffix(key, "}") {
		return key, ""
	}

	return key[:open], key[open+1 : len(key)-1]
}

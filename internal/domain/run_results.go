package domain

// RunResults is what Ballast keeps of a finished run.
//
// It is built from the k6 summary by the k6 adapter, which reads only the
// fields named here. Everything k6 also reports — and everything it may report
// in a future version — stays out.
type RunResults struct {
	// DurationMs is how long k6 says the run actually took.
	DurationMs float64
	Metrics    []RunMetric
	Thresholds []ThresholdResult
	Checks     []CheckResult
}

// RunMetric is one number k6 measured.
//
// A trend metric reports six of these — average, minimum, median, maximum and
// two percentiles — so the statistic is part of what identifies the row, not a
// property of it.
type RunMetric struct {
	// Metric is the k6 metric, with no tag selector: http_req_duration.
	Metric string
	// Tag is the selector of a submetric, empty for the metric as a whole:
	// name:POST /login, or expected_response:true.
	Tag string
	// Stat names which number this is: avg, p(95), count, rate, value.
	Stat  string
	Value float64
}

// ThresholdResult is k6's verdict on one threshold.
//
// Passed is read from the summary, never recomputed from the values. k6 owns
// the comparison; Ballast reporting a different answer than the tool that ran
// the test would be worse than reporting none.
type ThresholdResult struct {
	Metric     string
	Expression string
	Passed     bool
}

// CheckResult is how one check of the script fared.
type CheckResult struct {
	// Group is the path of enclosing groups, joined by ::, empty at the root.
	Group  string
	Name   string
	Passes int
	Fails  int
}

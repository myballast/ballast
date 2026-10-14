package domain

import (
	"fmt"
	"time"
)

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
	// Traffic is what the generator says it moved. It is also among Metrics,
	// as data_sent and data_received; it is pulled out here because summing a
	// month of traffic must not mean reading every metric of every run.
	Traffic Traffic
}

// Traffic is what a run moved, in bytes.
//
// One type for two measurements of the same thing, taken in two places: what
// the generator says it sent and received, and what the runner counted leaving
// for the target. The second is always the larger — it carries TLS records and
// every header — and their difference is the overhead this installation pays.
// They are the only figures Ballast keeps that end up in a price, so what is
// done with a strange one is a decision rather than a detail.
type Traffic struct {
	SentBytes     int64
	ReceivedBytes int64
	// Measured says the two above mean something. False means they are zero
	// because nothing could be counted — a summary whose counters cannot be
	// believed, a proxy whose totals could not be read — and not because
	// nothing moved. A run nobody measured is not a run that was free, and a
	// month's total has to be able to tell them apart.
	//
	// A run that made no request at all is measured, and moved nothing.
	Measured bool
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

// Usage is what an organization, or one of its projects, moved over a period.
//
// Four numbers rather than two, because they are two measurements of two
// different things: what left the machine, and what the generators said they
// sent. A price is built from the first; the second is what makes the first
// believable, since their ratio is the overhead and a ratio that moves without
// explanation means something changed that nobody meant to change.
type Usage struct {
	// From is included and To is not. Half-open, so two consecutive months
	// cannot both claim a run that ended at midnight.
	From, To time.Time

	// EgressBytes and IngressBytes were counted on the wire, by the proxy.
	EgressBytes  int64
	IngressBytes int64
	// DataSentBytes and DataReceivedBytes are what the generators reported.
	DataSentBytes     int64
	DataReceivedBytes int64

	// Runs is how many finished inside the period, and UnmeasuredRuns how many
	// of those nobody could measure. A total with no idea how much of it is
	// missing is a total nobody can price.
	Runs           int
	UnmeasuredRuns int
}

// ParseMonth reads a month as YYYY-MM and returns the period it names.
//
// In UTC, and not by accident: there is no timezone on an organization, and
// inventing one to answer "whose month?" would make a figure that is read as a
// cost depend on where the reader happens to be.
//
// The period is half-open — the first instant of the month, up to but not
// including the first instant of the next — which is the only way two
// consecutive months cannot both count a run that ended at midnight.
func ParseMonth(month string) (from, to time.Time, err error) {
	parsed, err := time.ParseInLocation("2006-01", month, time.UTC)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf(
			"%w: %q is not a month written as YYYY-MM", ErrInvalidMonth, month)
	}

	return parsed, parsed.AddDate(0, 1, 0), nil
}

// MonthOf is the period containing an instant, in UTC.
func MonthOf(at time.Time) (from, to time.Time) {
	utc := at.UTC()
	from = time.Date(utc.Year(), utc.Month(), 1, 0, 0, 0, 0, time.UTC)

	return from, from.AddDate(0, 1, 0)
}

package k6_test

import (
	"testing"

	"github.com/youssefmahersi/ballast/internal/adapter/k6"
	"github.com/youssefmahersi/ballast/internal/domain"
)

// Written before the extraction itself. These two counters are the only
// numbers in a k6 summary that will end up on an invoice, so what the parser
// does with a strange one matters more than usual.

func TestParseReadsTheTrafficCounters(t *testing.T) {
	results := parseFixture(t, readFixture(t))

	// The fixture's own numbers, as k6 wrote them.
	if results.Traffic.SentBytes != 89779 {
		t.Errorf("the run sent %d bytes, the summary says 89779", results.Traffic.SentBytes)
	}
	if results.Traffic.ReceivedBytes != 1186523 {
		t.Errorf("the run received %d bytes, the summary says 1186523", results.Traffic.ReceivedBytes)
	}
	if !results.Traffic.Measured {
		t.Error("a summary with two ordinary counters was not called measured")
	}

	// The same numbers are also kept as metrics, because they are metrics. The
	// columns on the run are a second copy for one reason — summing a month of
	// traffic must not mean reading every metric of every run — and two copies
	// of one fact have to agree.
	sent, ok := findMetric(results, "data_sent", "", "count")
	if !ok {
		t.Fatal("data_sent is missing from the metrics")
	}
	if int64(sent.Value) != results.Traffic.SentBytes {
		t.Errorf("the metric says %v and the column would say %d", sent.Value, results.Traffic.SentBytes)
	}
}

// A run that made no request at all. Nothing was measured, which is not the
// same thing as something being wrong.
func TestASummaryWithNoTrafficCountersMeasuresNothing(t *testing.T) {
	results, err := k6.NewSummaryParser().Parse([]byte(`{"metrics":{}}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if results.Traffic.SentBytes != 0 || results.Traffic.ReceivedBytes != 0 {
		t.Errorf("a summary with no counters measured %+v", results.Traffic)
	}
	if !results.Traffic.Measured {
		t.Error("absent counters were called unmeasured; absent is not the same as wrong")
	}
}

// The summary is written by a container running somebody's script. A counter
// that cannot be believed must not become a number in a column that will be
// summed into a price.
func TestACounterThatCannotBeBelievedMeasuresNothing(t *testing.T) {
	cases := map[string]string{
		"negative":        `{"metrics":{"data_sent":{"values":{"count":-1}},"data_received":{"values":{"count":10}}}}`,
		"beyond an int64": `{"metrics":{"data_sent":{"values":{"count":1e30}},"data_received":{"values":{"count":10}}}}`,
		"just beyond":     `{"metrics":{"data_sent":{"values":{"count":1e19}},"data_received":{"values":{"count":10}}}}`,
		"received is bad": `{"metrics":{"data_sent":{"values":{"count":10}},"data_received":{"values":{"count":-5}}}}`,
	}

	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			results, err := k6.NewSummaryParser().Parse([]byte(raw))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}

			// Both to zero, not just the bad one: a run measured by halves is
			// not measured, and a half-total summed into a month is worse
			// than a missing one.
			if results.Traffic.SentBytes != 0 || results.Traffic.ReceivedBytes != 0 {
				t.Errorf("an impossible counter was stored as %+v", results.Traffic)
			}
			if results.Traffic.Measured {
				t.Error("an impossible counter was reported as a measurement")
			}

			// The rest of the summary is untouched: a bad counter is not a
			// reason to throw away metrics and threshold verdicts.
			if len(results.Metrics) == 0 {
				t.Error("the metrics were lost along with the counter")
			}
		})
	}
}

// k6 writes counters as JSON numbers, so a fraction is possible. Bytes are
// whole, and truncating is the only answer that cannot invent traffic.
func TestAFractionalCounterIsTruncated(t *testing.T) {
	results, err := k6.NewSummaryParser().Parse(
		[]byte(`{"metrics":{"data_sent":{"values":{"count":123.9}},"data_received":{"values":{"count":7.2}}}}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if results.Traffic.SentBytes != 123 || results.Traffic.ReceivedBytes != 7 {
		t.Errorf("fractional counters became %+v", results.Traffic)
	}
	if !results.Traffic.Measured {
		t.Error("a fraction is not a reason to throw a counter away")
	}
}

// The parser's own vocabulary, kept out of the domain's: whatever k6 calls
// these metrics tomorrow, the domain still speaks of what was sent and
// received.
func TestTheTrafficIsReportedInTheDomainsTerms(t *testing.T) {
	var traffic domain.Traffic

	if traffic.SentBytes != 0 || traffic.ReceivedBytes != 0 || traffic.Measured {
		t.Fatalf("the zero value of Traffic is %+v", traffic)
	}
}

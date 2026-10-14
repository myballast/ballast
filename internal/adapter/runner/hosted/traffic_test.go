package hosted

import (
	"testing"
)

// Reading the proxy's totals out of its output, which is the one place the
// figure that will be charged for comes from.

func TestTheLastTotalsWin(t *testing.T) {
	// A running total every few seconds, and one more when the proxy is asked
	// to stop. They are answers to the same question at different moments, not
	// separate amounts to add up.
	output := []byte(`{"time":"2026-10-14T09:00:00Z","level":"INFO","msg":"serving the run's only way out","addr":":3128"}
{"time":"2026-10-14T09:00:05Z","level":"INFO","msg":"egress traffic","sent_bytes":1000,"received_bytes":20000}
{"time":"2026-10-14T09:00:10Z","level":"INFO","msg":"egress traffic","sent_bytes":4096,"received_bytes":65536}
`)

	traffic, err := lastTotals(output)
	if err != nil {
		t.Fatalf("lastTotals: %v", err)
	}
	if traffic.Sent != 4096 || traffic.Received != 65536 {
		t.Fatalf("the totals read as %+v", traffic)
	}
}

// The proxy's own messages share this stream, and so would anything the
// runtime printed. A line that is not the one being looked for is skipped, not
// a reason to give up on the ones that are.
func TestLinesThatAreNotTotalsAreSkipped(t *testing.T) {
	output := []byte(`not json at all
{"msg":"refused","host":"not-ours.example.test:443"}
{"msg":"egress traffic","sent_bytes":7,"received_bytes":9}
panic: something printed by the runtime
`)

	traffic, err := lastTotals(output)
	if err != nil {
		t.Fatalf("lastTotals: %v", err)
	}
	if traffic.Sent != 7 || traffic.Received != 9 {
		t.Fatalf("the totals read as %+v", traffic)
	}
}

// Nothing to read is an error rather than zero. Zero is a run that moved
// nothing; this is a run nobody measured, and the two must not look alike in a
// column that is summed into a price.
func TestOutputWithNoTotalsIsAnError(t *testing.T) {
	cases := map[string][]byte{
		"empty":           {},
		"only other logs": []byte(`{"msg":"serving the run's only way out"}` + "\n"),
		"half a line":     []byte(`{"msg":"egress traffic","sent_bytes":7}` + "\n"),
		"negative":        []byte(`{"msg":"egress traffic","sent_bytes":-7,"received_bytes":9}` + "\n"),
	}

	for name, output := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := lastTotals(output); err == nil {
				t.Fatal("unreadable output was read as a measurement")
			}
		})
	}
}

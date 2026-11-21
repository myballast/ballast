package domain

import "testing"

// What a pipeline reads: one word for "did the thresholds hold". The word is an
// addition of verdicts k6 already gave, and a run that has none says so instead
// of saying it passed.

func TestAThresholdVerdictAddsUpWhatK6Said(t *testing.T) {
	held := ThresholdResult{Metric: "http_req_duration", Expression: "p(95)<500", Passed: true}
	broken := ThresholdResult{Metric: "http_req_failed", Expression: "rate<0.01", Passed: false}

	cases := []struct {
		name       string
		thresholds []ThresholdResult
		want       ThresholdVerdict
	}{
		{"every threshold held", []ThresholdResult{held, held}, VerdictMet},
		{"one threshold held", []ThresholdResult{held}, VerdictMet},
		{"one broken among several is enough", []ThresholdResult{held, broken, held}, VerdictBreached},
		{"only a broken one", []ThresholdResult{broken}, VerdictBreached},
		// The case this type exists for: nothing was judged, so nothing was met.
		// A pipeline that read "met" here would go green on a run that never
		// measured anything against a rule.
		{"no threshold at all", nil, VerdictNone},
		{"an empty list", []ThresholdResult{}, VerdictNone},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := VerdictOf(c.thresholds); got != c.want {
				t.Errorf("VerdictOf = %q, want %q", got, c.want)
			}
		})
	}
}

// The words are part of the API: a pipeline compares against them.
func TestTheThresholdVerdictWordsAreTheDocumentedOnes(t *testing.T) {
	if VerdictMet != "met" || VerdictBreached != "breached" || VerdictNone != "none" {
		t.Errorf("words are %q %q %q, want met breached none", VerdictMet, VerdictBreached, VerdictNone)
	}
}

// It must agree with the run's own status verdict wherever both speak, so that
// the two words of one response cannot contradict each other.
func TestTheThresholdVerdictAgreesWithTheStatusVerdict(t *testing.T) {
	held := ThresholdResult{Passed: true}
	broken := ThresholdResult{Passed: false}

	for _, thresholds := range [][]ThresholdResult{{held}, {held, broken}, {broken}} {
		status := RunResults{Thresholds: thresholds}.Verdict()
		word := VerdictOf(thresholds)

		if (status == StatusPassed) != (word == VerdictMet) {
			t.Errorf("thresholds %v: status says %q and the verdict says %q", thresholds, status, word)
		}
	}
}

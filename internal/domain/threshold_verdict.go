package domain

// ThresholdVerdict is the one word a client reads to learn whether the
// thresholds of a run held.
//
// It is an addition of verdicts k6 already gave: whether a single threshold held
// is k6's judgement, read from the summary and stored, never recomputed here.
type ThresholdVerdict string

const (
	// VerdictMet means at least one threshold was judged, and every one held.
	VerdictMet ThresholdVerdict = "met"
	// VerdictBreached means at least one threshold did not hold.
	VerdictBreached ThresholdVerdict = "breached"
	// VerdictNone means nothing was judged: the run has no threshold, or has not
	// produced results — queued, running, errored, cancelled. It is not "met":
	// a pipeline that read success here would go green on a run that never
	// measured anything against a rule. The run's status says why there is none.
	VerdictNone ThresholdVerdict = "none"
)

// VerdictOf adds up the verdicts k6 gave.
//
// It is the only place they are added, and RunResults.Verdict, which picks the
// final status of a run, goes through it: the status of a run and the word a
// pipeline reads cannot disagree about the same thresholds.
func VerdictOf(thresholds []ThresholdResult) ThresholdVerdict {
	if len(thresholds) == 0 {
		return VerdictNone
	}
	for _, threshold := range thresholds {
		if !threshold.Passed {
			return VerdictBreached
		}
	}
	return VerdictMet
}

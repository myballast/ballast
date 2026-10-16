package domain

import (
	"math"
	"testing"
	"time"
)

var statsNow = time.Date(2026, 10, 16, 15, 30, 0, 0, time.UTC)

func day(offset int) time.Time {
	return time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC).AddDate(0, 0, offset)
}

func near(t *testing.T, name string, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Errorf("%s is nil, want %v", name, want)
		return
	}
	if math.Abs(*got-want) > 1e-9 {
		t.Errorf("%s = %v, want %v", name, *got, want)
	}
}

func isNil(t *testing.T, name string, got *float64) {
	t.Helper()
	if got != nil {
		t.Errorf("%s = %v, want nil: there is nothing to say, and 0 would say something", name, *got)
	}
}

// The windows are whole UTC days, today included, and the second is the same
// length right behind the first.
func TestPeriodsAreWholeUTCDaysEndingToday(t *testing.T) {
	p := PeriodsOf(statsNow, 30)

	if want := day(-29); !p.Current.From.Equal(want) {
		t.Errorf("current starts %v, want %v", p.Current.From, want)
	}
	if want := day(1); !p.Current.To.Equal(want) {
		t.Errorf("current ends %v, want the start of tomorrow, %v", p.Current.To, want)
	}
	if !p.Previous.To.Equal(p.Current.From) {
		t.Errorf("the previous window ends %v, want where the current one starts, %v", p.Previous.To, p.Current.From)
	}
	if got := p.Previous.To.Sub(p.Previous.From); got != 30*24*time.Hour {
		t.Errorf("the previous window lasts %v, want 30 days", got)
	}
}

func TestPeriodsReadTheInstantInUTC(t *testing.T) {
	// 23:30 on the 16th in Paris is 21:30 UTC, still the 16th; 01:30 on the 17th
	// in Paris is 23:30 UTC on the 16th. Both are the 16th.
	paris := time.FixedZone("CEST", 2*3600)
	late := time.Date(2026, 10, 17, 1, 30, 0, 0, paris)

	if got := PeriodsOf(late, 7).Current.To; !got.Equal(day(1)) {
		t.Errorf("the window ends %v, want the end of the 16th in UTC, %v", got, day(1))
	}
}

// A project with known runs, worked out by hand. Today and yesterday are in the
// current window; day(-31) is in the previous one.
//
//	today:      3 runs — 2 passed, 1 failed;  durations 60s and 120s, p95 400 and 600 ms
//	yesterday:  2 runs — 1 errored, 1 cancelled; the errored took 30s and has no p95
//	day(-31):   4 runs — 3 passed, 1 timeout;  durations 90s x3 and 150s; p95 300, 300, 300
func knownRuns() []DayStats {
	return []DayStats{
		{Day: day(0), StatsBucket: StatsBucket{
			Total: 3, Passed: 2, Failed: 1,
			DurationSumS: 60 + 120, DurationRuns: 2, P95Sum: 400 + 600, P95Runs: 2,
		}},
		{Day: day(-1), StatsBucket: StatsBucket{
			Total: 2, Failed: 1, DurationSumS: 30, DurationRuns: 1,
		}},
		{Day: day(-31), StatsBucket: StatsBucket{
			Total: 4, Passed: 3, Failed: 1,
			DurationSumS: 90*3 + 150, DurationRuns: 4, P95Sum: 900, P95Runs: 3,
		}},
	}
}

func TestTheFiguresAddUpTheRunsOfTheWindow(t *testing.T) {
	s := SummarizeStats(PeriodsOf(statsNow, 30), knownRuns())

	near(t, "total runs", s.TotalRuns.Value, 5)
	near(t, "failed runs", s.FailedRuns.Value, 2)
	// 2 passed of the 4 that had a verdict: the cancelled run is not one of them.
	near(t, "pass rate", s.PassRate.Value, 2.0/4.0)
	// (60+120+30) / 3 runs with a verdict.
	near(t, "average duration", s.AvgDurationS.Value, 70)
	// (400+600) / the 2 runs that reported one.
	near(t, "p95", s.P95Ms.Value, 500)
}

func TestTheChangeIsAgainstTheWindowBefore(t *testing.T) {
	s := SummarizeStats(PeriodsOf(statsNow, 30), knownRuns())

	near(t, "previous total", s.TotalRuns.Previous, 4)
	near(t, "total runs change (%)", s.TotalRuns.Change, 25) // 4 -> 5
	near(t, "failed runs change", s.FailedRuns.Change, 1)    // 1 -> 2
	// 3 of 4 passed before, 2 of 4 now: down 25 points.
	near(t, "pass rate change (points)", s.PassRate.Change, -25)
	// (90*3+150)/4 = 105 before, 70 now.
	near(t, "average duration change", s.AvgDurationS.Change, -35)
	near(t, "p95 change", s.P95Ms.Change, 200) // 300 -> 500
}

// The case the ticket names: nothing before to compare with. No change, and
// nothing that could be shown as NaN.
func TestAnEmptyPreviousWindowHasNoChange(t *testing.T) {
	rows := []DayStats{knownRuns()[0], knownRuns()[1]}

	s := SummarizeStats(PeriodsOf(statsNow, 30), rows)

	near(t, "total runs", s.TotalRuns.Value, 5)
	for name, m := range map[string]Measure{
		"pass rate": s.PassRate, "failed runs": s.FailedRuns, "total runs": s.TotalRuns,
		"average duration": s.AvgDurationS, "p95": s.P95Ms,
	} {
		isNil(t, name+" change", m.Change)
	}
	near(t, "total runs previous", s.TotalRuns.Previous, 0) // a real zero
	isNil(t, "pass rate previous", s.PassRate.Previous)
}

func TestAProjectWithNoRunHasNothingToSay(t *testing.T) {
	s := SummarizeStats(PeriodsOf(statsNow, 30), nil)

	near(t, "total runs", s.TotalRuns.Value, 0)
	near(t, "failed runs", s.FailedRuns.Value, 0)
	isNil(t, "pass rate", s.PassRate.Value)
	isNil(t, "average duration", s.AvgDurationS.Value)
	isNil(t, "p95", s.P95Ms.Value)
	for _, m := range []Measure{s.PassRate, s.FailedRuns, s.TotalRuns, s.AvgDurationS, s.P95Ms} {
		isNil(t, "change", m.Change)
	}
}

// A window with a verdict-free run in it has a total and no pass rate: not 0%.
func TestARunWithoutAVerdictIsNotAFailure(t *testing.T) {
	s := SummarizeStats(PeriodsOf(statsNow, 30), []DayStats{
		{Day: day(0), StatsBucket: StatsBucket{Total: 3}}, // queued, queued, cancelled
	})

	near(t, "total runs", s.TotalRuns.Value, 3)
	near(t, "failed runs", s.FailedRuns.Value, 0)
	isNil(t, "pass rate", s.PassRate.Value)
}

// Each figure has a point for each day, today last, so a curve can be drawn
// without asking for the days again.
func TestEachFigureHasAPointForEachDay(t *testing.T) {
	s := SummarizeStats(PeriodsOf(statsNow, 30), knownRuns())

	for name, m := range map[string]Measure{
		"pass rate": s.PassRate, "failed runs": s.FailedRuns, "total runs": s.TotalRuns,
		"average duration": s.AvgDurationS, "p95": s.P95Ms,
	} {
		if len(m.Series) != 30 {
			t.Errorf("%s has %d points, want 30", name, len(m.Series))
		}
	}

	near(t, "total today", s.TotalRuns.Series[29], 3)
	near(t, "total yesterday", s.TotalRuns.Series[28], 2)
	near(t, "total on a quiet day", s.TotalRuns.Series[10], 0) // a count, so a real zero
	near(t, "pass rate today", s.PassRate.Series[29], 2.0/3.0)
	isNil(t, "pass rate on a quiet day", s.PassRate.Series[10])
	isNil(t, "p95 yesterday", s.P95Ms.Series[28]) // no run reported one

	// The previous window is not drawn.
	var sum float64
	for _, v := range s.TotalRuns.Series {
		sum += *v
	}
	if sum != 5 {
		t.Errorf("the series add up to %v runs, want the 5 of the current window", sum)
	}
}

func TestADayOutsideBothWindowsIsIgnored(t *testing.T) {
	s := SummarizeStats(PeriodsOf(statsNow, 30), []DayStats{
		{Day: day(-100), StatsBucket: StatsBucket{Total: 50, Passed: 50}},
		{Day: day(2), StatsBucket: StatsBucket{Total: 9}}, // the future
	})

	near(t, "total runs", s.TotalRuns.Value, 0)
	near(t, "previous total", s.TotalRuns.Previous, 0)
}

func TestAWindowIsAsLongAsAsked(t *testing.T) {
	for _, days := range []int{1, 7, 30, 90} {
		s := SummarizeStats(PeriodsOf(statsNow, days), nil)
		if len(s.TotalRuns.Series) != days {
			t.Errorf("%d days gave %d points", days, len(s.TotalRuns.Series))
		}
	}
}

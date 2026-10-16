package domain

import "time"

// What a project's runs add up to over a stretch of days: the five figures the
// project's page shows, each against the stretch before it.
//
// The sums live here and not in the front end because they are rules, and a rule
// that two screens could compute differently is two rules. They are also the
// ones a reader checks against the list of runs, so they are written to be
// checked: each says plainly what it counts.

// StatsBucket is what the runs of one stretch — a day, or a window made of days
// — add up to. It holds sums and counts rather than averages, so that buckets
// can be added: an average of averages is not the average.
type StatsBucket struct {
	// Total is every run created in the stretch, whatever became of it.
	Total int
	// Passed and Failed are the runs that ended with a verdict. Failed is any
	// run that was meant to measure something and did not pass: failed,
	// errored or timed out. A run that was cancelled, rejected or skipped has
	// no verdict, and one still queued or running has not got one yet; none of
	// them says anything about the service under test, so they are in Total
	// and in neither.
	Passed int
	Failed int
	// DurationSumS and DurationRuns are the seconds between starting and
	// ending, and how many runs they come from: the runs with a verdict.
	DurationSumS float64
	DurationRuns int
	// P95Sum and P95Runs are the 95th percentile of the request duration, in
	// milliseconds, as k6 reported it for each run, and how many runs reported
	// one. A percentile of a whole period cannot be rebuilt from the percentile
	// of each run, so the figure is the mean of them, and says so.
	P95Sum  float64
	P95Runs int
}

// Add is the sum of two buckets.
func (b StatsBucket) Add(o StatsBucket) StatsBucket {
	return StatsBucket{
		Total:        b.Total + o.Total,
		Passed:       b.Passed + o.Passed,
		Failed:       b.Failed + o.Failed,
		DurationSumS: b.DurationSumS + o.DurationSumS,
		DurationRuns: b.DurationRuns + o.DurationRuns,
		P95Sum:       b.P95Sum + o.P95Sum,
		P95Runs:      b.P95Runs + o.P95Runs,
	}
}

// PassRate is the share of runs with a verdict that passed, between 0 and 1.
// Nil when none has one: nothing passed, and nothing failed, and 0% would say
// the second.
func (b StatsBucket) PassRate() *float64 {
	return ratio(float64(b.Passed), float64(b.Passed+b.Failed))
}

// AvgDurationS is how long a run with a verdict lasted, on average, in seconds.
func (b StatsBucket) AvgDurationS() *float64 {
	return ratio(b.DurationSumS, float64(b.DurationRuns))
}

// AvgP95Ms is the mean of the runs' 95th percentiles, in milliseconds.
func (b StatsBucket) AvgP95Ms() *float64 {
	return ratio(b.P95Sum, float64(b.P95Runs))
}

func ratio(sum, over float64) *float64 {
	if over == 0 {
		return nil
	}
	value := sum / over
	return &value
}

// DayStats is the bucket of one day, in UTC.
type DayStats struct {
	Day time.Time
	StatsBucket
}

// StatsWindow is a stretch of days: From included, To excluded, on midnights
// in UTC.
type StatsWindow struct {
	From time.Time
	To   time.Time
}

// StatsPeriods are the two stretches a figure is set against: the last Days
// days, today included, and the Days days before them.
//
// Days are UTC, as the month of the traffic measure is. A project belongs to
// no timezone, and "the last 30 days" that depended on where it was read from
// would give two people two sets of figures for the same runs.
type StatsPeriods struct {
	Days     int
	Previous StatsWindow
	Current  StatsWindow
}

// PeriodsOf is the two windows as of now.
func PeriodsOf(now time.Time, days int) StatsPeriods {
	today := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	currentFrom := today.AddDate(0, 0, -(days - 1))
	previousFrom := currentFrom.AddDate(0, 0, -days)

	return StatsPeriods{
		Days:     days,
		Previous: StatsWindow{From: previousFrom, To: currentFrom},
		Current:  StatsWindow{From: currentFrom, To: today.AddDate(0, 0, 1)},
	}
}

// Measure is one figure of the page: its value over the current window, the
// value before it, the change between them, and a point for each day of the
// current window to draw it from.
//
// A nil is "nothing to say", never zero: a day without a verdict has no pass
// rate, and a window set against an empty one has no change. A front end that
// shows a nil as "—" is showing the truth; one that shows 0 is not.
type Measure struct {
	Value    *float64
	Previous *float64
	// Change is how Value moved from Previous, in the unit that suits the
	// figure: percentage points for the pass rate, a percentage for the number
	// of runs, the figure's own unit for the others. Nil when either side is.
	Change *float64
	Series []*float64
}

// ProjectStats is the five figures of a project over a stretch of days.
type ProjectStats struct {
	Days         int
	Current      StatsWindow
	PassRate     Measure
	FailedRuns   Measure
	TotalRuns    Measure
	AvgDurationS Measure
	P95Ms        Measure
}

// SummarizeStats adds up a project's days into the five figures. days outside
// the two windows are ignored, and a day with no run need not be in rows.
func SummarizeStats(periods StatsPeriods, rows []DayStats) ProjectStats {
	current := make([]StatsBucket, periods.Days)
	var currentSum, previousSum StatsBucket

	for _, row := range rows {
		day := time.Date(row.Day.Year(), row.Day.Month(), row.Day.Day(), 0, 0, 0, 0, time.UTC)
		switch {
		case !day.Before(periods.Current.From) && day.Before(periods.Current.To):
			i := int(day.Sub(periods.Current.From).Hours() / 24)
			current[i] = current[i].Add(row.StatsBucket)
			currentSum = currentSum.Add(row.StatsBucket)
		case !day.Before(periods.Previous.From) && day.Before(periods.Previous.To):
			previousSum = previousSum.Add(row.StatsBucket)
		}
	}

	series := func(of func(StatsBucket) *float64) []*float64 {
		out := make([]*float64, len(current))
		for i, bucket := range current {
			out[i] = of(bucket)
		}
		return out
	}
	count := func(of func(StatsBucket) int) func(StatsBucket) *float64 {
		return func(b StatsBucket) *float64 {
			value := float64(of(b))
			return &value
		}
	}
	total := count(func(b StatsBucket) int { return b.Total })
	failed := count(func(b StatsBucket) int { return b.Failed })

	// The previous window is a basis for comparison only if it held runs. A
	// project that started last week has nothing to be compared with, and a
	// change measured against zero runs would be a number made of nothing.
	hadRuns := previousSum.Total > 0

	return ProjectStats{
		Days:    periods.Days,
		Current: periods.Current,
		PassRate: measure(currentSum.PassRate(), previousSum.PassRate(), series(StatsBucket.PassRate),
			func(now, before float64) *float64 { return pointer((now - before) * 100) }),
		FailedRuns: measure(failed(currentSum), failed(previousSum), series(failed),
			func(now, before float64) *float64 {
				if !hadRuns {
					return nil
				}
				return pointer(now - before)
			}),
		TotalRuns: measure(total(currentSum), total(previousSum), series(total),
			func(now, before float64) *float64 {
				if before == 0 {
					return nil
				}
				return pointer((now - before) / before * 100)
			}),
		AvgDurationS: measure(currentSum.AvgDurationS(), previousSum.AvgDurationS(), series(StatsBucket.AvgDurationS),
			func(now, before float64) *float64 { return pointer(now - before) }),
		P95Ms: measure(currentSum.AvgP95Ms(), previousSum.AvgP95Ms(), series(StatsBucket.AvgP95Ms),
			func(now, before float64) *float64 { return pointer(now - before) }),
	}
}

func measure(
	value, previous *float64, series []*float64, change func(now, before float64) *float64,
) Measure {
	m := Measure{Value: value, Previous: previous, Series: series}
	if value != nil && previous != nil {
		m.Change = change(*value, *previous)
	}
	return m
}

func pointer(v float64) *float64 { return &v }

//go:build integration

// What a run moved, stored and summed. Against a real database, because every
// rule here is a WHERE clause — and because what it adds up is what somebody
// will be charged.
package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// moved records a finished run that moved what it was given, ended at the
// instant given, and returns it.
func (w queueWorld) moved(
	ctx context.Context, t *testing.T, at time.Time, wire, reported domain.Traffic,
) domain.Run {
	t.Helper()

	run := w.queue(ctx, t, w.env)

	results := domain.RunResults{
		Metrics: []domain.RunMetric{
			{Metric: "data_sent", Stat: "count", Value: float64(reported.SentBytes)},
			{Metric: "data_received", Stat: "count", Value: float64(reported.ReceivedBytes)},
		},
		Traffic: reported,
	}
	if err := w.runs.SaveResults(ctx, w.org, run.ID, results, wire); err != nil {
		t.Fatalf("SaveResults: %v", err)
	}

	// A run is counted in a month by when it ended, and only a finished run has
	// volumes at all.
	if err := w.db.InTenantTx(ctx, w.org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE run SET status = 'passed', ended_at = $3 WHERE org_id = $1 AND id = $2`,
			w.org, run.ID, at)
		return err
	}); err != nil {
		t.Fatalf("finish the run: %v", err)
	}

	return run
}

func measured(sent, received int64) domain.Traffic {
	return domain.Traffic{SentBytes: sent, ReceivedBytes: received, Measured: true}
}

// Both measurements are stored, side by side, and the copy on the run agrees
// with the metric it came from — they are written in one transaction from one
// parsed summary, and this is what says so.
func TestWhatARunMovedIsStoredTwiceAndAgrees(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

	run := world.moved(ctx, t, at, measured(1500, 21000), measured(1000, 20000))

	var egress, ingress, sent, received int64
	var flagged bool
	if err := world.db.InTenantTx(ctx, world.org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT egress_bytes, ingress_bytes,
			data_sent_bytes, data_received_bytes, traffic_measured
			FROM run WHERE org_id = $1 AND id = $2`, world.org, run.ID).
			Scan(&egress, &ingress, &sent, &received, &flagged)
	}); err != nil {
		t.Fatalf("read the run: %v", err)
	}

	switch {
	case egress != 1500 || ingress != 21000:
		t.Errorf("the wire figures are %d and %d", egress, ingress)
	case sent != 1000 || received != 20000:
		t.Errorf("the generator's figures are %d and %d", sent, received)
	case !flagged:
		t.Error("a run both measurements were taken of is not marked measured")
	}

	// The same numbers, as metrics. Two copies of one fact that must agree.
	results, err := world.runs.Results(ctx, world.org, run.ID)
	if err != nil {
		t.Fatalf("Results: %v", err)
	}
	for _, metric := range results.Metrics {
		if metric.Metric == "data_sent" && int64(metric.Value) != sent {
			t.Errorf("the metric says %v and the column says %d", metric.Value, sent)
		}
	}
}

// A run only one measurement was taken of is unmeasured. Half a figure summed
// into a month would read as a cheap month.
func TestARunMeasuredByHalvesIsNotMeasured(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

	// The proxy's totals could not be read; the generator's were fine.
	world.moved(ctx, t, at, domain.Traffic{}, measured(1000, 20000))

	from, to, _ := domain.ParseMonth("2026-10")
	usage, err := world.runs.TrafficInPeriod(ctx, world.org, from, to)
	if err != nil {
		t.Fatalf("TrafficInPeriod: %v", err)
	}

	if usage.Runs != 1 || usage.UnmeasuredRuns != 1 {
		t.Fatalf("the month counted %d runs, %d of them unmeasured", usage.Runs, usage.UnmeasuredRuns)
	}
	if usage.EgressBytes != 0 {
		t.Errorf("an unread proxy produced %d bytes of egress", usage.EgressBytes)
	}
	// What was measured is still there: the generator's figures are not thrown
	// away because the other half is missing.
	if usage.DataSentBytes != 1000 {
		t.Errorf("the generator's figure was lost: %d", usage.DataSentBytes)
	}
}

// The month is half-open, and that is the whole of it: a run that ended at the
// first instant of November belongs to November and to nothing else.
func TestAMonthCountsItsOwnRunsAndNoOthers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	october, november, _ := domain.ParseMonth("2026-10")

	// The last instant of October, and the first of November.
	world.moved(ctx, t, november.Add(-time.Microsecond), measured(100, 1000), measured(90, 900))
	world.moved(ctx, t, november, measured(500, 5000), measured(450, 4500))
	// And one before the month started.
	world.moved(ctx, t, october.Add(-time.Second), measured(7, 7), measured(7, 7))

	usage, err := world.runs.TrafficInPeriod(ctx, world.org, october, november)
	if err != nil {
		t.Fatalf("TrafficInPeriod: %v", err)
	}

	if usage.Runs != 1 {
		t.Fatalf("october counted %d runs, wanted 1", usage.Runs)
	}
	if usage.EgressBytes != 100 || usage.IngressBytes != 1000 {
		t.Fatalf("october counted %d out and %d in", usage.EgressBytes, usage.IngressBytes)
	}
}

// A run still going has no end and no volumes. It is counted in the month it
// ends in, if it ever does.
func TestARunThatHasNotEndedIsInNoMonth(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	world.queue(ctx, t, world.env)

	from, to := domain.MonthOf(time.Now())
	usage, err := world.runs.TrafficInPeriod(ctx, world.org, from, to)
	if err != nil {
		t.Fatalf("TrafficInPeriod: %v", err)
	}
	if usage.Runs != 0 {
		t.Fatalf("a queued run was counted in a month: %+v", usage)
	}
}

// One project's figure is one project's.
func TestAProjectsTrafficIsItsOwn(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	world.moved(ctx, t, at, measured(100, 1000), measured(90, 900))

	from, to, _ := domain.ParseMonth("2026-10")

	mine, err := world.runs.ProjectTrafficInPeriod(ctx, world.org, world.scenario.ProjectID, from, to)
	if err != nil {
		t.Fatalf("ProjectTrafficInPeriod: %v", err)
	}
	if mine.EgressBytes != 100 {
		t.Fatalf("the project moved %d bytes", mine.EgressBytes)
	}

	elsewhere, err := world.runs.ProjectTrafficInPeriod(ctx, world.org, uuid.New(), from, to)
	if err != nil {
		t.Fatalf("ProjectTrafficInPeriod: %v", err)
	}
	if elsewhere.Runs != 0 {
		t.Fatalf("another project's figure is %+v", elsewhere)
	}
}

// And one organization's is unreadable from another. A figure that leaks tells
// a competitor how much traffic a company's tests generate.
func TestOneOrganizationsTrafficIsUnreadableByAnother(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	theirs := newQueueWorld(ctx, t)
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	theirs.moved(ctx, t, at, measured(9999, 99999), measured(9000, 90000))

	ours := newQueueWorld(ctx, t)
	from, to, _ := domain.ParseMonth("2026-10")

	usage, err := ours.runs.TrafficInPeriod(ctx, ours.org, from, to)
	if err != nil {
		t.Fatalf("TrafficInPeriod: %v", err)
	}
	if usage.Runs != 0 || usage.EgressBytes != 0 {
		t.Fatalf("another organization's traffic was readable: %+v", usage)
	}

	// Which is not the figure having vanished: its own organization still has it.
	stillTheirs, err := theirs.runs.TrafficInPeriod(ctx, theirs.org, from, to)
	if err != nil {
		t.Fatalf("TrafficInPeriod: %v", err)
	}
	if stillTheirs.EgressBytes != 9999 {
		t.Fatalf("their own figure reads as %+v", stillTheirs)
	}
}

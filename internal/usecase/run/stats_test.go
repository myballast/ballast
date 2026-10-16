package run

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// the clock of listService: 1970. The windows are read from it, not from the
// wall clock.
func statsService(runs *fakeRuns, authz Authorizer) *Service {
	return listService(runs, authz)
}

func TestProjectStatsReadsBothWindowsInOneQuery(t *testing.T) {
	runs := &fakeRuns{}
	project := uuid.New()

	stats, err := statsService(runs, allowAll{}).ProjectStats(context.Background(), someone(), project, 30)
	if err != nil {
		t.Fatalf("ProjectStats: %v", err)
	}

	periods := domain.PeriodsOf(time.Unix(0, 0).UTC(), 30)
	if runs.statsFor != project {
		t.Errorf("read the stats of %s, want %s", runs.statsFor, project)
	}
	if !runs.statsFrom.Equal(periods.Previous.From) || !runs.statsTo.Equal(periods.Current.To) {
		t.Errorf("read %v to %v, want from the start of the previous window, %v, to the end of the current one, %v",
			runs.statsFrom, runs.statsTo, periods.Previous.From, periods.Current.To)
	}
	if stats.Days != 30 || len(stats.TotalRuns.Series) != 30 {
		t.Errorf("days %d, %d points", stats.Days, len(stats.TotalRuns.Series))
	}
}

func TestProjectStatsAddsUpWhatTheRepositoryReturned(t *testing.T) {
	at := time.Unix(0, 0).UTC()
	runs := &fakeRuns{dailyStats: []domain.DayStats{
		{Day: at, StatsBucket: domain.StatsBucket{Total: 4, Passed: 3, Failed: 1}},
	}}

	stats, err := statsService(runs, allowAll{}).ProjectStats(context.Background(), someone(), uuid.New(), 30)
	if err != nil {
		t.Fatalf("ProjectStats: %v", err)
	}
	if stats.TotalRuns.Value == nil || *stats.TotalRuns.Value != 4 {
		t.Errorf("total runs %v, want 4", stats.TotalRuns.Value)
	}
	if stats.PassRate.Value == nil || *stats.PassRate.Value != 0.75 {
		t.Errorf("pass rate %v, want 0.75", stats.PassRate.Value)
	}
}

func TestProjectStatsBoundsTheDays(t *testing.T) {
	for _, tc := range []struct{ asked, want int }{
		{0, DefaultStatsDays},
		{-5, DefaultStatsDays},
		{1, 1},
		{7, 7},
		{MaxStatsDays, MaxStatsDays},
		{MaxStatsDays + 1, MaxStatsDays},
		{100_000, MaxStatsDays},
	} {
		stats, err := statsService(&fakeRuns{}, allowAll{}).ProjectStats(context.Background(), someone(), uuid.New(), tc.asked)
		if err != nil {
			t.Fatalf("ProjectStats: %v", err)
		}
		if stats.Days != tc.want || len(stats.TotalRuns.Series) != tc.want {
			t.Errorf("asked %d days, got %d (%d points), want %d", tc.asked, stats.Days, len(stats.TotalRuns.Series), tc.want)
		}
	}
}

func TestProjectStatsRefusesWithoutViewProject(t *testing.T) {
	runs := &fakeRuns{}

	_, err := statsService(runs, refuseAll{}).ProjectStats(context.Background(), someone(), uuid.New(), 30)
	if !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("got %v, want ErrForbidden", err)
	}
	if runs.statsFor != uuid.Nil {
		t.Error("the runs were added up before the permission was checked")
	}
}

func TestProjectStatsAsksOnlyForViewProject(t *testing.T) {
	asked := &askedFor{}
	project := uuid.New()

	_, _ = statsService(&fakeRuns{}, asked).ProjectStats(context.Background(), someone(), project, 30)

	if asked.perm != domain.PermViewProject || asked.projectID != project {
		t.Errorf("asked %v on %s, want view_project on %s", asked.perm, asked.projectID, project)
	}
}

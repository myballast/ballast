package run

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// chattyExecutor stands in for a generator that writes output before it
// finishes. It writes into the channel it was handed, which is the channel the
// service substituted for the caller's.
type chattyExecutor struct {
	lines   []domain.LogLine
	outcome Outcome
	err     error
}

func (e *chattyExecutor) Execute(
	ctx context.Context, _ Job, logs chan<- domain.LogLine,
) (Outcome, error) {
	for _, line := range e.lines {
		select {
		case logs <- line:
		case <-ctx.Done():
			return Outcome{}, ctx.Err()
		}
	}

	return e.outcome, e.err
}

// wrote is a generator's output, numbered the way the runner numbers it: one
// counter shared between the two streams.
func wrote(messages ...string) []domain.LogLine {
	lines := make([]domain.LogLine, 0, len(messages))
	for i, message := range messages {
		lines = append(lines, domain.LogLine{
			Seq:     int64(i),
			Stream:  domain.StreamStdout,
			Message: message,
			At:      time.Unix(int64(i), 0).UTC(),
		})
	}
	return lines
}

// What a viewer arriving mid-run will read. The first acceptance criterion of
// the ticket rests on this: nothing can be shown from the beginning that was
// never stored.
func TestWhatTheGeneratorWritesIsStoredAsItIsWritten(t *testing.T) {
	f := newExecutionFixture()
	f.service.executor = &chattyExecutor{
		lines:   wrote("starting", "  ✓ status is 200", "done"),
		outcome: Outcome{Summary: []byte(`{"metrics":{}}`)},
	}

	f.process(t)

	stored := f.logs.stored()
	if len(stored) != 3 {
		t.Fatalf("%d line(s) were stored, the generator wrote 3", len(stored))
	}
	for i, line := range stored {
		if line.Seq != int64(i) {
			t.Fatalf("the stored lines are numbered %d, %d, %d",
				stored[0].Seq, stored[1].Seq, stored[2].Seq)
		}
	}
	if stored[1].Message != "  ✓ status is 200" {
		t.Fatalf("the second line says %q", stored[1].Message)
	}
}

// Censored once, and the censored line is what goes everywhere. Redacting only
// on the way to the table would make "no credential in the logs" true of the
// database and false of the worker's own output.
func TestASecretIsCensoredBeforeItIsStoredAndBeforeItIsPassedOn(t *testing.T) {
	f := newExecutionFixture()
	// The fixture's environment carries API_TOKEN=a-token, and the script
	// prints it — which a script somebody uploaded is free to do.
	f.service.executor = &chattyExecutor{
		lines:   wrote("calling with token a-token", "Authorization: Bearer abc.def"),
		outcome: Outcome{Summary: []byte(`{"metrics":{}}`)},
	}

	// The worker's own channel, drained the way cmd/worker drains it.
	forwarded := make(chan domain.LogLine, 8)
	worked, err := f.service.ProcessNext(context.Background(), f.orgID, forwarded)
	if err != nil {
		t.Fatalf("ProcessNext: %v", err)
	}
	if !worked {
		t.Fatal("the worker found nothing to do")
	}
	close(forwarded)

	for _, line := range f.logs.stored() {
		if strings.Contains(line.Message, "a-token") || strings.Contains(line.Message, "abc.def") {
			t.Errorf("a credential was stored: %q", line.Message)
		}
	}

	var passed int
	for line := range forwarded {
		passed++
		if strings.Contains(line.Message, "a-token") || strings.Contains(line.Message, "abc.def") {
			t.Errorf("a credential was passed to the worker: %q", line.Message)
		}
	}
	if passed != 2 {
		t.Fatalf("%d line(s) reached the worker, the generator wrote 2", passed)
	}
}

// The ordering that makes a finished run's logs complete: a viewer who sees the
// run end and reads one last time must not be missing its last line.
func TestTheLastLineIsStoredBeforeTheRunIsRecordedAsFinished(t *testing.T) {
	f := newExecutionFixture()
	// Long enough that no batch is written on a timer: the only thing that can
	// store these lines is the end of the execution waiting for them, which is
	// exactly what this test is about. With a fast timer the lines would be
	// stored in time by accident, and the test would pass whatever the order.
	f.service.flushEvery = time.Hour
	// And a write that takes as long as a write does, so that finishing the
	// run without waiting for it would be visible rather than lucky.
	f.logs.delay = 20 * time.Millisecond
	f.service.executor = &chattyExecutor{
		lines:   wrote("starting", "done"),
		outcome: Outcome{Summary: []byte(`{"metrics":{}}`)},
	}

	f.process(t)

	events := f.journal().recorded()
	finished := slices.Index(events, "finished")
	if finished < 0 {
		t.Fatalf("the run was never recorded as finished: %v", events)
	}
	lastStored := -1
	for i, event := range events {
		if event == "stored" {
			lastStored = i
		}
	}
	if lastStored > finished {
		t.Fatalf("a line was stored after the run was finished: %v", events)
	}
	if !slices.Contains(events[:finished], "stored") {
		t.Fatalf("nothing was stored before the run was finished: %v", events)
	}
}

// A run of a project the caller cannot see has no logs, for the same reason it
// has no run: a refusal would confirm that the identifier stands for something.
func TestLogsOfARunTheCallerMayNotSeeReadAsAbsent(t *testing.T) {
	service, runs, existing, who := cancelFixture(domain.StatusRunning, domain.RunCancelled)
	logs := &fakeLogs{since: wrote("secret output")}
	service.logs = logs
	service.authz = refuseAll{}
	_ = runs

	_, _, err := service.LogsSince(context.Background(), who, existing.ID, -1, 100)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
	if logs.limit != 0 {
		t.Fatal("the lines of a run the caller may not see were read anyway")
	}
}

// The resumption point goes through untouched, and the page is capped: a client
// asking for everything must not be handed a run's entire history at once.
func TestTheResumptionPointGoesThroughAndThePageIsCapped(t *testing.T) {
	service, _, existing, who := cancelFixture(domain.StatusRunning, domain.RunCancelled)
	logs := &fakeLogs{since: wrote("one", "two")}
	service.logs = logs

	if _, _, err := service.LogsSince(context.Background(), who, existing.ID, 41, 10); err != nil {
		t.Fatalf("LogsSince: %v", err)
	}
	if logs.after != 41 || logs.limit != 10 {
		t.Fatalf("the store was asked for lines after %d, limit %d", logs.after, logs.limit)
	}

	for _, asked := range []int{0, -1, MaxLogPage * 10} {
		if _, _, err := service.LogsSince(context.Background(), who, existing.ID, 0, asked); err != nil {
			t.Fatalf("LogsSince: %v", err)
		}
		if logs.limit != MaxLogPage {
			t.Fatalf("a page of %d was asked for and the store got %d", asked, logs.limit)
		}
	}
}

// The status comes back with the lines, because it is what tells a stream
// whether to keep waiting: terminal and nothing left means there will never be
// another line.
func TestTheRunsStatusComesBackWithItsLines(t *testing.T) {
	service, _, existing, who := cancelFixture(domain.StatusPassed, domain.RunCancelled)
	service.logs = &fakeLogs{since: wrote("done")}

	lines, status, err := service.LogsSince(context.Background(), who, existing.ID, -1, 100)
	if err != nil {
		t.Fatalf("LogsSince: %v", err)
	}
	if status != domain.StatusPassed {
		t.Fatalf("the status came back as %q", status)
	}
	if len(lines) != 1 {
		t.Fatalf("%d line(s) came back", len(lines))
	}
}

// A store that cannot be written to does not take the run down with it: output
// is not a measurement, and a run generating load must not stop because a line
// could not be filed.
func TestAStoreThatRefusesALineDoesNotStopTheRun(t *testing.T) {
	f := newExecutionFixture()
	f.logs.appendErr = errors.New("the database is not answering")
	f.service.executor = &chattyExecutor{
		lines:   wrote("starting", "done"),
		outcome: Outcome{Summary: []byte(`{"metrics":{}}`)},
	}

	f.process(t)

	if f.runs.finished != domain.StatusPassed {
		t.Fatalf("the run ended as %q, wanted passed", f.runs.finished)
	}
}

// The identifiers the store is given are the run's own, so that nothing is ever
// filed under another organization.
func TestTheLinesAreFiledUnderTheRunsOwnOrganization(t *testing.T) {
	f := newExecutionFixture()
	f.service.executor = &chattyExecutor{
		lines:   wrote("starting"),
		outcome: Outcome{Summary: []byte(`{"metrics":{}}`)},
	}

	f.process(t)

	if f.logs.orgID != f.orgID || f.logs.runID != f.claimed.ID {
		t.Fatalf("the lines were filed under org %s run %s, wanted %s and %s",
			f.logs.orgID, f.logs.runID, f.orgID, f.claimed.ID)
	}
}

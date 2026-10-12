//go:build integration

// The lines a run wrote, stored and read back: the ordering the live view
// follows, the resumption that must not repeat itself, and the isolation that
// keeps one organization's output out of another's screen.
package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/adapter/postgres"
	"github.com/youssefmahersi/ballast/internal/domain"
)

// wrote is what a generator produced, in the order it produced it: one counter
// shared between the two streams, as the runner assigns them.
func wrote(at time.Time, messages ...string) []domain.LogLine {
	lines := make([]domain.LogLine, 0, len(messages))
	for i, message := range messages {
		stream := domain.StreamStdout
		if i%3 == 2 {
			stream = domain.StreamStderr
		}
		lines = append(lines, domain.LogLine{
			Seq:     int64(i),
			Stream:  stream,
			Message: message,
			At:      at.Add(time.Duration(i) * time.Millisecond),
		})
	}
	return lines
}

func storedLineCount(ctx context.Context, t *testing.T, world queueWorld, runID uuid.UUID) int {
	t.Helper()

	var stored int
	if err := world.db.InTenantTx(ctx, world.org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM run_log WHERE org_id = $1 AND run_id = $2`,
			world.org, runID).Scan(&stored)
	}); err != nil {
		t.Fatalf("count the lines: %v", err)
	}
	return stored
}

func TestTheLinesComeBackInTheOrderTheyWereWritten(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	logs := postgres.NewLogRepository(world.db)
	run := world.queue(ctx, t, world.env)

	// Truncated because PostgreSQL keeps microseconds in a timestamptz and Go
	// keeps nanoseconds.
	at := time.Now().UTC().Truncate(time.Microsecond)
	written := wrote(at, "starting", "  ✓ status is 200", "WARN something", "done")
	if err := logs.Append(ctx, world.org, run.ID, written); err != nil {
		t.Fatalf("Append: %v", err)
	}

	read, err := logs.Since(ctx, world.org, run.ID, -1, 100)
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	if len(read) != len(written) {
		t.Fatalf("%d line(s) came back, wrote %d", len(read), len(written))
	}

	for i, line := range read {
		want := written[i]
		switch {
		case line.Seq != want.Seq:
			t.Fatalf("line %d is numbered %d", i, line.Seq)
		case line.Message != want.Message:
			t.Fatalf("line %d says %q, wrote %q", i, line.Message, want.Message)
		case line.Stream != want.Stream:
			t.Fatalf("line %d came from %q, wrote %q", i, line.Stream, want.Stream)
		case !line.At.Equal(want.At):
			t.Fatalf("line %d is stamped %s, wrote %s", i, line.At, want.At)
		}
	}
}

// The resumption, which is the second acceptance criterion of the ticket read
// at this level: after is exclusive, so a client that holds up to 1 gets 2
// onwards and nothing it already has.
func TestReadingAfterASequenceNumberRepeatsNothing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	logs := postgres.NewLogRepository(world.db)
	run := world.queue(ctx, t, world.env)

	at := time.Now().UTC().Truncate(time.Microsecond)
	if err := logs.Append(ctx, world.org, run.ID,
		wrote(at, "zero", "one", "two", "three")); err != nil {
		t.Fatalf("Append: %v", err)
	}

	read, err := logs.Since(ctx, world.org, run.ID, 1, 100)
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	if len(read) != 2 || read[0].Seq != 2 || read[1].Seq != 3 {
		t.Fatalf("resuming after 1 gave %+v", read)
	}

	// And the end of the stream reads as nothing left rather than as an error.
	read, err = logs.Since(ctx, world.org, run.ID, 3, 100)
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	if len(read) != 0 {
		t.Fatalf("resuming after the last line gave %+v", read)
	}
}

// A page at a time: the first read of a run that wrote a hundred thousand lines
// must not be a hundred thousand lines.
func TestOnlyAPageIsReadAtATime(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	logs := postgres.NewLogRepository(world.db)
	run := world.queue(ctx, t, world.env)

	at := time.Now().UTC().Truncate(time.Microsecond)
	messages := make([]string, 0, 10)
	for i := range 10 {
		messages = append(messages, string(rune('a'+i)))
	}
	if err := logs.Append(ctx, world.org, run.ID, wrote(at, messages...)); err != nil {
		t.Fatalf("Append: %v", err)
	}

	read, err := logs.Since(ctx, world.org, run.ID, -1, 3)
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	if len(read) != 3 || read[0].Seq != 0 || read[2].Seq != 2 {
		t.Fatalf("a page of three gave %+v", read)
	}
}

// A worker whose acknowledgement was lost writes its batch again. It has no way
// of knowing it arrived, so the statement has to be the thing that knows.
func TestABatchWrittenTwiceIsStoredOnce(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	world := newQueueWorld(ctx, t)
	logs := postgres.NewLogRepository(world.db)
	run := world.queue(ctx, t, world.env)

	at := time.Now().UTC().Truncate(time.Microsecond)
	written := wrote(at, "zero", "one", "two")

	for range 2 {
		if err := logs.Append(ctx, world.org, run.ID, written); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	if stored := storedLineCount(ctx, t, world, run.ID); stored != 3 {
		t.Fatalf("%d line(s) are stored, wrote 3 twice", stored)
	}

	// And the overlap of two batches — the ordinary case of a retry that sent
	// one line too few last time — stores the new lines and leaves the old.
	if err := logs.Append(ctx, world.org, run.ID,
		wrote(at, "zero", "one", "two", "three")); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if stored := storedLineCount(ctx, t, world, run.ID); stored != 4 {
		t.Fatalf("%d line(s) are stored after an overlapping batch, wanted 4", stored)
	}
}

// Nothing new about row-level security, and that is the point of checking: the
// live log view is a new way to read rows, and a new way to read rows is a new
// way to read somebody else's.
func TestOneOrganizationsLinesAreNotReadableByAnother(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	theirs := newQueueWorld(ctx, t)
	theirLogs := postgres.NewLogRepository(theirs.db)
	theirRun := theirs.queue(ctx, t, theirs.env)

	at := time.Now().UTC().Truncate(time.Microsecond)
	if err := theirLogs.Append(ctx, theirs.org, theirRun.ID,
		wrote(at, "their output")); err != nil {
		t.Fatalf("Append: %v", err)
	}

	ours := newQueueWorld(ctx, t)
	ourLogs := postgres.NewLogRepository(ours.db)

	// The run identifier is known here, which is the only interesting case: a
	// reader who guesses one must still see nothing.
	read, err := ourLogs.Since(ctx, ours.org, theirRun.ID, -1, 100)
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	if len(read) != 0 {
		t.Fatalf("another organization's lines were readable: %+v", read)
	}

	// Which is not the lines having vanished: their own reader still has them.
	stillTheirs, err := theirLogs.Since(ctx, theirs.org, theirRun.ID, -1, 100)
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	if len(stillTheirs) != 1 {
		t.Fatalf("their own lines read as %+v", stillTheirs)
	}
}

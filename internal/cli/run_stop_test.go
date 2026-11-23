package cli

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// A pipeline is stopped for two reasons: it ran out of the time it was given, or a
// person or a runner told it to stop with a signal. In both the run it started is
// load on somebody's target, and leaving it behind is the one thing the tool must
// not do silently.
//
// What these tests hold in place:
//
//   - the run is asked to stop before the tool leaves, once, and the tool leaves
//     with 2 whatever the answer was;
//   - asking is all it does: a run under way is stopped by the worker running it,
//     so the tool never says "cancelled" for what is only requested;
//   - a second signal is believed: the tool leaves at once, and says what it left;
//   - an interruption before the run has a name cannot ask for anything, and says
//     that a run may exist;
//   - a run that is already over is not cancelled, and its result is still given.
//
// Written before the code, which is why they do not compile yet.

// running answers every read of the run with "running", for ever.
func running() func(int, http.ResponseWriter) {
	return pollsIn(state("running", "none"))
}

func TestTheDefaultTimeoutIsTwoHoursAndANegativeOneIsRefused(t *testing.T) {
	if DefaultTimeout != 2*time.Hour {
		t.Errorf("the default timeout is %s, want 2h: a run lasts an hour at most, and the queue takes some", DefaultTimeout)
	}
	if got := (RunOptions{}).timeout(); got != DefaultTimeout {
		t.Errorf("no timeout means %s, want the default", got)
	}
	if got := (RunOptions{Timeout: 90 * time.Minute}).timeout(); got != 90*time.Minute {
		t.Errorf("a timeout of 90m means %s", got)
	}

	b := &ballast{}
	got := wholeRun(t, b, func(o *RunOptions, _ *Runner) { o.Timeout = -time.Second })

	if got.code != ExitError || !strings.Contains(got.err, "--timeout") || b.posts.Load() != 0 {
		t.Errorf("exit %d, %d launch(es):\n%s", got.code, b.posts.Load(), got.err)
	}
}

// --- The timeout -----------------------------------------------------------------------------------------

func TestARunThatTakesLongerThanTheTimeoutIsAskedToStopAndTheToolLeavesWithTwo(t *testing.T) {
	b := &ballast{poll: running()}

	start := time.Now()
	got := wholeRun(t, b, func(o *RunOptions, _ *Runner) { o.Timeout = 150 * time.Millisecond })

	if got.code != ExitError {
		t.Fatalf("exit %d, want 2\nstderr: %s", got.code, got.err)
	}
	if b.deletes.Load() != 1 {
		t.Errorf("the run was asked to stop %d times, want once", b.deletes.Load())
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Errorf("the tool left after %s with a timeout of 150ms", took)
	}
	if !strings.Contains(got.err, "timeout") || !strings.Contains(got.err, theRunID) {
		t.Errorf("nothing says why it stopped or which run:\n%s", got.err)
	}
	if !strings.Contains(got.all(), thePageURL) {
		t.Errorf("no link to the run, which is the place to see how it ended:\n%s", got.all())
	}
	// It does not read the run again after it gave up on it.
	reads := b.polls.Load()
	time.Sleep(60 * time.Millisecond)
	if b.polls.Load() != reads {
		t.Error("the tool went on reading the run after it left")
	}
}

func TestARunThatFinishesInTimeIsNeverCancelled(t *testing.T) {
	b := &ballast{poll: pollsIn(state("running", "none"), state("passed", "met", held))}

	got := wholeRun(t, b, func(o *RunOptions, _ *Runner) { o.Timeout = 5 * time.Second })

	if got.code != ExitMet || b.deletes.Load() != 0 {
		t.Errorf("exit %d, %d cancellation(s)", got.code, b.deletes.Load())
	}
}

// The timeout covers the whole of it, the launch as well as the wait: a pipeline
// that gave this long to a run did not mean the tool to wait longer for it to start.
func TestATimeoutDuringTheLaunchSaysARunMayHaveBeenQueued(t *testing.T) {
	release := make(chan struct{})
	b := &ballast{launch: func(_ http.ResponseWriter) { <-release }}
	defer close(release)

	got := wholeRun(t, b, func(o *RunOptions, _ *Runner) { o.Timeout = 150 * time.Millisecond })

	if got.code != ExitError {
		t.Fatalf("exit %d, want 2", got.code)
	}
	if !strings.Contains(got.err, "check the project's runs") || !strings.Contains(got.err, "timeout") {
		t.Errorf("nothing says a run may exist:\n%s", got.err)
	}
	// There is no name to ask for anything with.
	if b.deletes.Load() != 0 || b.posts.Load() != 1 {
		t.Errorf("%d cancellation(s), %d launch(es)", b.deletes.Load(), b.posts.Load())
	}
}

// --- The first signal --------------------------------------------------------------------------------------

func TestAFirstSignalAsksTheRunToStopBeforeTheToolLeaves(t *testing.T) {
	closed := make(chan struct{})
	b := &ballast{
		poll: running(),
		logs: func(w http.ResponseWriter, r *http.Request) {
			startStream(w)
			sendLine(w, 0, "under way")
			<-r.Context().Done()
			close(closed)
		},
	}
	ctx, signal := context.WithCancel(context.Background())
	go func() {
		time.Sleep(150 * time.Millisecond)
		signal()
	}()

	got := wholeRunCtx(ctx, t, b, nil)

	if got.code != ExitError {
		t.Fatalf("exit %d, want 2\nstderr: %s", got.code, got.err)
	}
	if b.deletes.Load() != 1 {
		t.Errorf("the run was asked to stop %d times, want once", b.deletes.Load())
	}
	if !strings.Contains(got.err, "interrupted") || !strings.Contains(got.err, theRunID) {
		t.Errorf("nothing says why it stopped or which run:\n%s", got.err)
	}
	if !strings.Contains(got.out, "under way") {
		t.Errorf("what the run had written before is lost:\n%s", got.out)
	}
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Error("the stream was left open")
	}
}

// A signal that arrives while the launch is in the air: the request may have
// landed, nothing says it did, and there is nothing to ask about.
func TestASignalDuringTheLaunchSaysARunMayHaveBeenQueued(t *testing.T) {
	release := make(chan struct{})
	b := &ballast{launch: func(_ http.ResponseWriter) { <-release }}
	defer close(release)

	ctx, signal := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		signal()
	}()

	got := wholeRunCtx(ctx, t, b, nil)

	if got.code != ExitError {
		t.Fatalf("exit %d, want 2", got.code)
	}
	if !strings.Contains(got.err, "interrupted") || !strings.Contains(got.err, "check the project's runs") {
		t.Errorf("nothing says a run may exist:\n%s", got.err)
	}
	if b.deletes.Load() != 0 || b.posts.Load() != 1 {
		t.Errorf("%d cancellation(s), %d launch(es)", b.deletes.Load(), b.posts.Load())
	}
	if strings.Contains(got.err, "context canceled") {
		t.Errorf("the message is the toolchain's and not the tool's:\n%s", got.err)
	}
}

// What the request to stop is answered with is told as it is. Asking is all that
// is done, and a run under way is stopped by the worker running it a moment later:
// "cancelled" is not said of what is only requested.
func TestTheAnswerToTheRequestToStopIsToldAsItIs(t *testing.T) {
	cases := []struct {
		name   string
		answer func(w http.ResponseWriter)
		want   []string
		not    []string
	}{
		{"a run under way is asked", func(w http.ResponseWriter) {
			writeJSON(w, 202, `{"id":"`+theRunID+`","status":"running","outcome":"cancellation_requested"}`)
		}, []string{"asked"}, []string{"was cancelled", "is cancelled", "has been cancelled"}},
		{"a queued run is over", func(w http.ResponseWriter) {
			writeJSON(w, 200, `{"id":"`+theRunID+`","status":"cancelled","outcome":"cancelled"}`)
		}, []string{"cancelled"}, nil},
		{"a run that had already finished", func(w http.ResponseWriter) {
			writeJSON(w, 409, `{"error":"this run has already finished"}`)
		}, []string{"had already finished when it was asked to stop"}, []string{"could not stop"}},
		{"a run that is not visible any more", func(w http.ResponseWriter) {
			writeJSON(w, 404, `{"error":"no such run"}`)
		}, []string{"could not stop", "may still be running"}, nil},
		{"a token that is refused", func(w http.ResponseWriter) {
			writeJSON(w, 401, `{"error":"unauthenticated"}`)
		}, []string{"could not stop", "may still be running"}, nil},
		{"a server that fails", func(w http.ResponseWriter) {
			writeJSON(w, 500, `{"error":"oops"}`)
		}, []string{"could not", "may still be running"}, nil},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := &ballast{poll: running(), cancel: c.answer}
			ctx, signal := context.WithCancel(context.Background())
			go func() {
				time.Sleep(100 * time.Millisecond)
				signal()
			}()

			got := wholeRunCtx(ctx, t, b, nil)

			if got.code != ExitError {
				t.Errorf("exit %d, want 2: whatever the answer, a run that was interrupted is not a success", got.code)
			}
			for _, want := range c.want {
				if !strings.Contains(got.err, want) {
					t.Errorf("stderr lacks %q:\n%s", want, got.err)
				}
			}
			for _, not := range c.not {
				if strings.Contains(got.err, not) {
					t.Errorf("stderr says %q of a run that was only asked to stop:\n%s", not, got.err)
				}
			}
		})
	}
}

// Ballast not answering is the one case no signal is there to cut short, and the
// request to stop must have a limit of its own: a pipeline that was told to stop
// does not wait for ever on the thing it is trying to stop.
func TestARequestToStopThatIsNeverAnsweredIsGivenUpOn(t *testing.T) {
	release := make(chan struct{})
	b := &ballast{poll: running(), cancel: func(_ http.ResponseWriter) { <-release }}
	defer close(release)

	ctx, signal := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		signal()
	}()

	start := time.Now()
	got := wholeRunCtx(ctx, t, b, func(_ *RunOptions, r *Runner) { r.StopWait = 200 * time.Millisecond })

	if got.code != ExitError {
		t.Errorf("exit %d, want 2", got.code)
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Errorf("the tool waited %s for an answer it was told to wait 200ms for", took)
	}
	if !strings.Contains(got.err, "may still be running") {
		t.Errorf("it left without saying what it left:\n%s", got.err)
	}
}

// --- The second signal --------------------------------------------------------------------------------------

func TestASecondSignalIsBelievedAndTheToolLeavesAtOnce(t *testing.T) {
	release := make(chan struct{})
	b := &ballast{
		poll:   running(),
		cancel: func(_ http.ResponseWriter) { <-release }, // Ballast does not answer
	}
	defer close(release)

	abort := make(chan struct{})
	ctx, first := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		first()
		time.Sleep(150 * time.Millisecond)
		close(abort)
	}()

	start := time.Now()
	got := wholeRunCtx(ctx, t, b, func(_ *RunOptions, r *Runner) { r.Abort = abort })

	if got.code != ExitError {
		t.Fatalf("exit %d, want 2", got.code)
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Errorf("the tool took %s, with the second signal 250ms in: the request to stop is waited for up to ten seconds, and a second signal is not believed", took)
	}
	if !strings.Contains(got.err, "may still be running") || !strings.Contains(got.err, theRunID) {
		t.Errorf("it left without saying what it left:\n%s", got.err)
	}
	if b.deletes.Load() != 1 {
		t.Errorf("the run was asked to stop %d times, want once", b.deletes.Load())
	}
}

// --- A run that is already over ------------------------------------------------------------------------------

// The run ended and its output is still being waited for when the signal comes:
// there is nothing left to stop, and its result is what the pipeline asked for.
func TestASignalWhileTheEndOfAFinishedRunIsWaitedForStillGivesItsResult(t *testing.T) {
	b := &ballast{
		poll: pollsIn(state("passed", "met", held)),
		logs: func(w http.ResponseWriter, r *http.Request) {
			startStream(w)
			sendLine(w, 0, "last words")
			<-r.Context().Done() // the end event never comes
		},
	}
	ctx, signal := context.WithCancel(context.Background())
	go func() {
		time.Sleep(150 * time.Millisecond)
		signal()
	}()

	start := time.Now()
	got := wholeRunCtx(ctx, t, b, func(_ *RunOptions, r *Runner) { r.DrainWait = 10 * time.Second })

	if got.code != ExitMet {
		t.Errorf("exit %d, want 0\nstderr: %s", got.code, got.err)
	}
	if b.deletes.Load() != 0 {
		t.Errorf("a run that was over was asked to stop")
	}
	if !strings.Contains(got.out, "http_req_duration") {
		t.Errorf("the result was not given:\n%s", got.out)
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("the result was given after %s: the wait for the end of the output was not cut short", time.Since(start))
	}
}

// --- What is printed ------------------------------------------------------------------------------------------

func TestTheTokenIsNeverInWhatIsSaidWhenTheToolStops(t *testing.T) {
	echo := " " + aToken + " and Authorization: Bearer " + aToken
	b := &ballast{
		poll: running(),
		cancel: func(w http.ResponseWriter) {
			writeJSON(w, 500, `{"error":"broke`+strings.ReplaceAll(echo, `"`, ``)+`"}`)
		},
	}
	ctx, signal := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		signal()
	}()

	got := wholeRunCtx(ctx, t, b, nil)

	if strings.Contains(got.all(), aToken) || strings.Contains(got.all(), aToken[4:]) {
		t.Errorf("the token was printed:\nstdout: %s\nstderr: %s", got.out, got.err)
	}
	if got.code != ExitError {
		t.Errorf("exit %d", got.code)
	}
}

// countingWriter is a destination that can be read while it is being written to,
// and counts what it received.
type countingWriter struct {
	mu sync.Mutex
	n  int
}

func (c *countingWriter) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n += len(p)
	return len(p), nil
}

func (c *countingWriter) bytes() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

// Nothing is written once the tool has left: what follows the output of the run is
// stopped before Run returns, and a goroutine that went on writing into a closed
// log would be one the race detector sees.
func TestNothingIsWrittenAfterTheToolHasLeft(t *testing.T) {
	b := &ballast{
		poll: running(),
		logs: func(w http.ResponseWriter, r *http.Request) {
			startStream(w)
			for i := 0; i < 5000 && r.Context().Err() == nil; i++ {
				sendLine(w, i, fmt.Sprintf("line %d", i))
				time.Sleep(time.Millisecond)
			}
		},
	}
	ctx, signal := context.WithCancel(context.Background())
	go func() {
		time.Sleep(150 * time.Millisecond)
		signal()
	}()

	var sink countingWriter
	got := wholeRunCtx(ctx, t, b, func(_ *RunOptions, r *Runner) { r.Out = &sink })
	atReturn := sink.bytes()
	time.Sleep(150 * time.Millisecond)

	if got.code != ExitError || atReturn == 0 {
		t.Errorf("exit %d, %d bytes written", got.code, atReturn)
	}
	if after := sink.bytes(); after != atReturn {
		t.Errorf("%d bytes were written after the tool had left", after-atReturn)
	}
}

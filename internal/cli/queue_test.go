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

// These hold what the tool does when Ballast says "not now" (429) to a launch.
//
// A 429 is a refusal that comes before anything is created: nothing was queued, so
// asking again cannot make two runs. That is what makes it the one answer worth
// repeating. An outcome that is not known (a cut connection, a 5xx, a signal during the
// call) is never repeated: nothing distinguishes a retry from a second run.
//
// The waits are recorded, not slept, so that these tests do not take minutes.
//
// Written before the code, which is why they do not compile yet.

// tooBusy answers 429 the first n times, then queues the run.
func tooBusy(n int, retryAfter string, message string) func(*ballast) func(http.ResponseWriter) {
	return func(b *ballast) func(http.ResponseWriter) {
		return func(w http.ResponseWriter) {
			if int(b.posts.Load()) > n {
				writeJSON(w, 202, fmt.Sprintf(`{"id":%q,"status":"queued"}`, theRunID))
				return
			}
			if retryAfter != "" {
				w.Header().Set("Retry-After", retryAfter)
			}
			writeJSON(w, 429, fmt.Sprintf(`{"error":%q}`, message))
		}
	}
}

// waits is what a test learns about the pauses: what each was asked to be.
type waits struct {
	mu   sync.Mutex
	list []time.Duration
}

func (w *waits) record(_ context.Context, d time.Duration) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.list = append(w.list, d)
	return true
}

func (w *waits) all() []time.Duration {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]time.Duration(nil), w.list...)
}

func queueing(timeout time.Duration, w *waits) func(*RunOptions, *Runner) {
	return func(o *RunOptions, r *Runner) {
		o.QueueTimeout = timeout
		r.LaunchWait = w.record
		r.LaunchBackoff = func(attempt int) time.Duration { return time.Duration(attempt) * time.Minute }
	}
}

func TestALaunchRefusedForTheLimitsIsRepeatedUntilItIsAccepted(t *testing.T) {
	b := &ballast{}
	b.launch = tooBusy(2, "", "20 runs are already queued")(b)
	w := &waits{}

	got := wholeRun(t, b, queueing(10*time.Minute, w))

	if got.code != ExitMet {
		t.Fatalf("exit %d, want 0\nstdout: %s\nstderr: %s", got.code, got.out, got.err)
	}
	if b.posts.Load() != 3 {
		t.Errorf("the launch was sent %d times, want 3 (two refusals, then the run)", b.posts.Load())
	}
	if len(w.all()) != 2 {
		t.Errorf("the tool paused %d times, want 2: %v", len(w.all()), w.all())
	}
	// The pipeline reads the log: it must say why nothing happens.
	for _, want := range []string{"20 runs are already queued", "Trying again in", "launched run " + theRunID} {
		if !strings.Contains(got.err, want) {
			t.Errorf("stderr lacks %q:\n%s", want, got.err)
		}
	}
	if got.out != "" && strings.Contains(got.out, "20 runs") {
		t.Errorf("the tool's own words reached stdout: %q", got.out)
	}
}

func TestTheWaitFollowsRetryAfterWhenTheServerGivesOne(t *testing.T) {
	b := &ballast{}
	b.launch = tooBusy(1, "90", "you have started 30 runs in the last hour, the most allowed")(b)
	w := &waits{}

	got := wholeRun(t, b, queueing(10*time.Minute, w))

	if got.code != ExitMet {
		t.Fatalf("exit %d\n%s", got.code, got.err)
	}
	if list := w.all(); len(list) != 1 || list[0] != 90*time.Second {
		t.Errorf("the pause was %v, want [1m30s], what Retry-After said", list)
	}
}

func TestTheWaitBacksOffWhenTheServerGivesNoDuration(t *testing.T) {
	b := &ballast{}
	b.launch = tooBusy(3, "", "20 runs are already queued")(b)
	w := &waits{}

	got := wholeRun(t, b, queueing(time.Hour, w))

	if got.code != ExitMet {
		t.Fatalf("exit %d\n%s", got.code, got.err)
	}
	want := []time.Duration{time.Minute, 2 * time.Minute, 3 * time.Minute}
	list := w.all()
	if len(list) != len(want) {
		t.Fatalf("pauses %v, want %v", list, want)
	}
	for i := range want {
		if list[i] != want[i] {
			t.Errorf("pause %d was %s, want %s (the pause the policy gave for that try)", i+1, list[i], want[i])
		}
	}
}

func TestAGuessedPauseIsCutToWhatIsLeftOfTheBudget(t *testing.T) {
	// The tool's own guess may not outlast the time it was given: only what the
	// server said it needs can say "this will not fit".
	b := &ballast{}
	b.launch = tooBusy(100, "", "20 runs are already queued")(b)
	w := &waits{}

	got := wholeRun(t, b, queueing(150*time.Second, w))

	if got.code != ExitError {
		t.Fatalf("exit %d, want 2", got.code)
	}
	var total time.Duration
	for _, d := range w.all() {
		total += d
	}
	if total != 150*time.Second {
		t.Errorf("the tool waited %s in all, want exactly the 2m30s it was given: %v", total, w.all())
	}
	for _, want := range []string{"20 runs are already queued", "--queue-timeout"} {
		if !strings.Contains(got.err, want) {
			t.Errorf("stderr lacks %q:\n%s", want, got.err)
		}
	}
}

func TestARetryAfterLongerThanTheBudgetLeavesAtOnceAndSaysHowLong(t *testing.T) {
	b := &ballast{}
	b.launch = tooBusy(100, "2460", "you have started 30 runs in the last hour, the most allowed")(b)
	w := &waits{}

	got := wholeRun(t, b, queueing(10*time.Minute, w))

	if got.code != ExitError {
		t.Fatalf("exit %d, want 2", got.code)
	}
	if len(w.all()) != 0 {
		t.Errorf("the tool paused %v although the wait could not fit", w.all())
	}
	if b.posts.Load() != 1 {
		t.Errorf("the launch was sent %d times, want once: waiting to fail anyway helps nobody", b.posts.Load())
	}
	for _, want := range []string{"30 runs in the last hour", "41m", "--queue-timeout", "10m"} {
		if !strings.Contains(got.err, want) {
			t.Errorf("stderr lacks %q:\n%s", want, got.err)
		}
	}
	if b.polls.Load() != 0 || b.deletes.Load() != 0 {
		t.Error("a run that never existed was read or cancelled")
	}
}

func TestWithNoQueueTimeoutTheFirstRefusalIsFinal(t *testing.T) {
	b := &ballast{}
	b.launch = tooBusy(100, "30", "20 runs are already queued")(b)
	w := &waits{}

	got := wholeRun(t, b, queueing(0, w))

	if got.code != ExitError || b.posts.Load() != 1 || len(w.all()) != 0 {
		t.Errorf("exit %d, %d launches, pauses %v: with a queue timeout of 0 the tool must leave at the first refusal",
			got.code, b.posts.Load(), w.all())
	}
}

func TestOnlyATooBusyAnswerIsRepeated(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"a target that is not verified", 403, `{"error":"this environment has not been verified"}`},
		{"a mistake in the request", 422, `{"error":"unknown profile"}`},
		{"the switch", 503, `{"error":"Ballast is not accepting new runs"}`},
		{"a token that is no good", 401, `{"error":"unauthenticated"}`},
		{"an outcome that is not known", 502, `<html>bad gateway</html>`},
		{"an error of the server", 500, `{"error":"boom"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := &ballast{launch: func(w http.ResponseWriter) { writeJSON(w, c.status, c.body) }}
			w := &waits{}

			got := wholeRun(t, b, queueing(10*time.Minute, w))

			if got.code != ExitError {
				t.Errorf("exit %d, want 2", got.code)
			}
			if b.posts.Load() != 1 || len(w.all()) != 0 {
				t.Errorf("%d launches and pauses %v: only a 429 may be asked again", b.posts.Load(), w.all())
			}
		})
	}
}

func TestASignalWhileWaitingForRoomLeavesWithTwoAndSaysNothingWasLaunched(t *testing.T) {
	b := &ballast{}
	b.launch = tooBusy(100, "", "20 runs are already queued")(b)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got := wholeRunCtx(ctx, t, b, func(o *RunOptions, r *Runner) {
		o.QueueTimeout = 10 * time.Minute
		r.LaunchBackoff = func(int) time.Duration { return time.Hour }
		// The first pause is the moment the signal arrives.
		r.LaunchWait = func(ctx context.Context, _ time.Duration) bool {
			cancel()
			<-ctx.Done()
			return false
		}
	})

	if got.code != ExitError {
		t.Fatalf("exit %d, want 2", got.code)
	}
	if !strings.Contains(got.err, "no run was launched") {
		t.Errorf("nothing says that no run exists:\n%s", got.err)
	}
	if strings.Contains(got.err, "may have reached Ballast") || strings.Contains(got.err, "check the project's runs") {
		t.Errorf("the tool claims a run may exist, when its last request was answered:\n%s", got.err)
	}
	if b.posts.Load() != 1 || b.deletes.Load() != 0 {
		t.Errorf("%d launches, %d cancellations: no request was in flight and nothing was launched", b.posts.Load(), b.deletes.Load())
	}
}

func TestTheTimeoutEndsTheWaitForRoomToo(t *testing.T) {
	b := &ballast{}
	b.launch = tooBusy(1000, "", "20 runs are already queued")(b)

	got := wholeRun(t, b, func(o *RunOptions, r *Runner) {
		o.QueueTimeout = time.Hour
		o.Timeout = 80 * time.Millisecond
		r.LaunchBackoff = func(int) time.Duration { return time.Hour }
		// The real pause: ended by the context, as the real tool's is.
	})

	if got.code != ExitError {
		t.Fatalf("exit %d, want 2", got.code)
	}
	if !strings.Contains(got.err, "timeout of 80ms") || !strings.Contains(got.err, "no run was launched") {
		t.Errorf("the timeout was not named, or the tool did not say that nothing was launched:\n%s", got.err)
	}
}

func TestTheServersSentenceIsPrintedAndNothingElseOfAnOrganisation(t *testing.T) {
	// The server writes the figure of the caller's own organisation. The tool adds
	// only its own numbers (what it waited, what it was allowed): never a "margin"
	// that it would have to have learned elsewhere.
	b := &ballast{}
	b.launch = tooBusy(100, "2460", "your organisation has started 100 runs in the last hour, the most allowed")(b)

	got := wholeRun(t, b, queueing(10*time.Minute, &waits{}))

	if !strings.Contains(got.err, "your organisation has started 100 runs in the last hour") {
		t.Errorf("the server's own sentence is missing:\n%s", got.err)
	}
}

func TestAHostileMessageStaysSafeWhileWaiting(t *testing.T) {
	b := &ballast{}
	b.launch = tooBusy(1, "", "busy \x1b[31m\u202e blt_0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")(b)

	got := wholeRun(t, b, queueing(10*time.Minute, &waits{}))

	if strings.ContainsAny(got.err, "\x1b\u202e") {
		t.Errorf("a control or invisible character got through:\n%q", got.err)
	}
	if strings.Contains(got.err, "blt_0123456789abcdef") {
		t.Errorf("a token-shaped string was printed:\n%q", got.err)
	}
}

func TestARunThatStaysQueuedSaysItIsStillQueuedWithHowLong(t *testing.T) {
	b := &ballast{poll: func(n int, w http.ResponseWriter) {
		if n < 12 {
			writeJSON(w, 200, state("queued", "none"))
			return
		}
		writeJSON(w, 200, state("passed", "met", held))
	}}
	got := wholeRun(t, b, func(_ *RunOptions, r *Runner) {
		r.QueuedNotice = 20 * time.Millisecond
		r.PollInterval = 10 * time.Millisecond
	})

	if got.code != ExitMet {
		t.Fatalf("exit %d\n%s", got.code, got.err)
	}
	if strings.Count(got.err, "status: queued") != 1 {
		t.Errorf("the status line was printed %d times, want once:\n%s", strings.Count(got.err, "status: queued"), got.err)
	}
	if n := strings.Count(got.err, "still queued"); n < 2 || n > 8 {
		t.Errorf("%d \"still queued\" notices, want a few, spaced by the interval:\n%s", n, got.err)
	}
	if !strings.Contains(got.err, "another run on this environment") {
		t.Errorf("the notice does not say what a queued run waits for:\n%s", got.err)
	}
}

func TestARunThatLeavesTheQueueIsNotToldItIsStillQueued(t *testing.T) {
	b := &ballast{poll: pollsIn(state("queued", "none"), state("running", "none"), state("passed", "met", held))}

	got := wholeRun(t, b, func(_ *RunOptions, r *Runner) { r.QueuedNotice = time.Nanosecond })

	if strings.Contains(got.err, "still queued") {
		t.Errorf("a run that started was said to be queued again:\n%s", got.err)
	}
}

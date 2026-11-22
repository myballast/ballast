package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The tool follows a run's output the way a browser's EventSource would, and does
// the part a browser does for itself: when the connection drops it reconnects with
// the number of the last line it read, and the server answers with what follows.
//
// What these tests hold in place:
//
//   - every line is printed once and in order, however many times the connection
//     drops — none twice, none skipped;
//   - an ending is told apart from a drop: the `end` event means the run is over,
//     a closed connection on its own means "try again";
//   - what cannot be fixed by trying again is not tried again: a token that is no
//     longer good, a run that is not visible, a stream the server stopped;
//   - what can be is tried a bounded number of times, with a pause that grows;
//   - nothing the server sends, in a line or in an error, can carry the token out.
//
// The stream is a convenience and never the verdict: a failure of it is reported to
// the caller, who decides it must not fail the run.
//
// Written before the code, which is why they do not compile yet.

// --- A server that streams -------------------------------------------------------------------------

type scripted struct {
	hits     atomic.Int32
	mu       sync.Mutex
	headers  []http.Header
	lastSeen []string
}

func (s *scripted) record(r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.headers = append(s.headers, r.Header.Clone())
	s.lastSeen = append(s.lastSeen, r.Header.Get("Last-Event-ID"))
}

func (s *scripted) lastEventIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.lastSeen...)
}

// streamer answers every connection with script(attempt, writer), the attempt
// being 1 for the first.
func streamer(t *testing.T, script func(attempt int, w http.ResponseWriter, r *http.Request), opts ...Option) (*Client, *scripted) {
	t.Helper()

	state := &scripted{}
	client, _ := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		attempt := int(state.hits.Add(1))
		state.record(r)
		script(attempt, w, r)
	}, append([]Option{WithStreamBackoff(func(int) time.Duration { return time.Millisecond })}, opts...)...)
	return client, state
}

func startStream(w http.ResponseWriter) http.Flusher {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher := w.(http.Flusher)
	flusher.Flush()
	return flusher
}

func sendLine(w http.ResponseWriter, seq int, message string) {
	data, _ := json.Marshal(map[string]any{
		"seq": seq, "stream": "stdout", "message": message,
		"at": fmt.Sprintf("2026-11-22T10:00:%02dZ", seq%60),
	})
	_, _ = fmt.Fprintf(w, "id: %d\nevent: line\ndata: %s\n\n", seq, data)
	w.(http.Flusher).Flush()
}

func sendEnd(w http.ResponseWriter, status string) {
	_, _ = fmt.Fprintf(w, "event: end\ndata: {\"status\":%q}\n\n", status)
	w.(http.Flusher).Flush()
}

// collect runs Follow and returns what it emitted.
func collect(ctx context.Context, client *Client) ([]LogLine, StreamEnd, error) {
	var lines []LogLine
	end, err := client.Follow(ctx, "r1", func(line LogLine) { lines = append(lines, line) })
	return lines, end, err
}

func messages(lines []LogLine) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = line.Message
	}
	return out
}

// --- Reading ------------------------------------------------------------------------------------------

func TestTheLinesOfARunAreEmittedInOrderUntilItEnds(t *testing.T) {
	client, state := streamer(t, func(_ int, w http.ResponseWriter, _ *http.Request) {
		startStream(w)
		sendLine(w, 0, "starting")
		_, _ = fmt.Fprint(w, ": keep-alive\n\n")
		sendLine(w, 1, "  ✓ status is 200")
		sendLine(w, 2, "done")
		sendEnd(w, "passed")
	})

	lines, end, err := collect(context.Background(), client)
	if err != nil {
		t.Fatalf("Follow: %v", err)
	}

	if got := strings.Join(messages(lines), "|"); got != "starting|  ✓ status is 200|done" {
		t.Errorf("lines %q", got)
	}
	if lines[0].Seq != 0 || lines[2].Seq != 2 || lines[0].Stream != "stdout" || lines[0].At.IsZero() {
		t.Errorf("a line lost what it carried: %+v", lines[0])
	}
	if end.Status != "passed" {
		t.Errorf("the run ended %q, want passed", end.Status)
	}
	if state.hits.Load() != 1 {
		t.Errorf("%d connection(s), want one", state.hits.Load())
	}
}

func TestTheStreamIsAskedForAsAnEventStreamWithTheToken(t *testing.T) {
	client, state := streamer(t, func(_ int, w http.ResponseWriter, _ *http.Request) {
		startStream(w)
		sendEnd(w, "passed")
	})

	if _, _, err := collect(context.Background(), client); err != nil {
		t.Fatalf("Follow: %v", err)
	}

	header := state.headers[0]
	if header.Get("Authorization") != "Bearer "+aToken || header.Get("Accept") != "text/event-stream" {
		t.Errorf("headers %v", header)
	}
	if got := state.lastEventIDs()[0]; got != "" {
		t.Errorf("the first connection sent Last-Event-ID %q: a run is shown from its first line", got)
	}
}

// The protocol allows more than the server happens to send, and a proxy may
// rewrite what it sends.
func TestWhatTheProtocolAllowsIsReadAsIt(t *testing.T) {
	client, _ := streamer(t, func(_ int, w http.ResponseWriter, _ *http.Request) {
		startStream(w)
		// CRLF line endings, no space after the colon, an unknown field, an unknown
		// event, a comment in the middle of an event, and a data field split in two.
		_, _ = fmt.Fprint(w, "retry: 3000\r\n")
		_, _ = fmt.Fprint(w, "id:0\r\nevent:line\r\n: a comment inside an event\r\nfoo: bar\r\ndata:{\"seq\":0,\"stream\":\"stderr\",\r\ndata:\"message\":\"split\",\"at\":\"2026-11-22T10:00:00Z\"}\r\n\r\n")
		_, _ = fmt.Fprint(w, "event: something_new\ndata: {}\n\n")
		_, _ = fmt.Fprint(w, "event: end\ndata: {\"status\":\"failed\"}\n\n")
		w.(http.Flusher).Flush()
	})

	lines, end, err := collect(context.Background(), client)
	if err != nil {
		t.Fatalf("Follow: %v", err)
	}

	if len(lines) != 1 || lines[0].Message != "split" || lines[0].Stream != "stderr" {
		t.Errorf("lines %+v", lines)
	}
	if end.Status != "failed" {
		t.Errorf("end %+v", end)
	}
}

// --- A connection that drops ----------------------------------------------------------------------------

func TestADroppedStreamIsResumedFromTheLastLineReadWithNoneRepeatedAndNoneSkipped(t *testing.T) {
	client, state := streamer(t, func(attempt int, w http.ResponseWriter, r *http.Request) {
		startStream(w)
		switch attempt {
		case 1:
			sendLine(w, 0, "zero")
			sendLine(w, 1, "one")
			sendLine(w, 2, "two")
			// The connection is lost: no end event, nothing but silence.
		case 2:
			// The server resumes after what the client said it had.
			from, _ := strconv.Atoi(r.Header.Get("Last-Event-ID"))
			for seq := from + 1; seq <= 5; seq++ {
				sendLine(w, seq, []string{"zero", "one", "two", "three", "four", "five"}[seq])
			}
			sendEnd(w, "passed")
		}
	})

	lines, end, err := collect(context.Background(), client)
	if err != nil {
		t.Fatalf("Follow: %v", err)
	}

	if got := strings.Join(messages(lines), ","); got != "zero,one,two,three,four,five" {
		t.Errorf("lines %q: some were repeated or skipped", got)
	}
	if end.Status != "passed" {
		t.Errorf("end %+v", end)
	}
	ids := state.lastEventIDs()
	if len(ids) != 2 || ids[0] != "" || ids[1] != "2" {
		t.Errorf("Last-Event-ID was %q, want none and then 2", ids)
	}
}

// The server is the one that decides where to resume from, and a server that gets
// it wrong must not make a line appear twice on the screen.
func TestALineTheServerSendsAgainIsNotPrintedTwice(t *testing.T) {
	client, _ := streamer(t, func(attempt int, w http.ResponseWriter, _ *http.Request) {
		startStream(w)
		if attempt == 1 {
			sendLine(w, 0, "zero")
			sendLine(w, 1, "one")
			return
		}
		sendLine(w, 0, "zero")
		sendLine(w, 1, "one")
		sendLine(w, 2, "two")
		sendEnd(w, "passed")
	})

	lines, _, err := collect(context.Background(), client)
	if err != nil {
		t.Fatalf("Follow: %v", err)
	}

	if got := strings.Join(messages(lines), ","); got != "zero,one,two" {
		t.Errorf("lines %q", got)
	}
}

func TestAnEndAfterADropIsStillAnEnd(t *testing.T) {
	client, _ := streamer(t, func(attempt int, w http.ResponseWriter, _ *http.Request) {
		startStream(w)
		if attempt == 1 {
			sendLine(w, 0, "zero")
			return
		}
		// The run finished while the connection was down: nothing is left to send
		// but the ending.
		sendEnd(w, "errored")
	})

	lines, end, err := collect(context.Background(), client)
	if err != nil || end.Status != "errored" || len(lines) != 1 {
		t.Errorf("lines %v, end %+v, err %v", messages(lines), end, err)
	}
}

// Every connection that gets somewhere is a reason to keep going: a long run on a
// network that drops often is not a stream that gave up.
func TestAStreamThatKeepsMakingProgressIsNeverGivenUpOn(t *testing.T) {
	const drops = 12 // well past the number of failures in a row that is tolerated

	client, state := streamer(t, func(attempt int, w http.ResponseWriter, _ *http.Request) {
		startStream(w)
		if attempt <= drops {
			sendLine(w, attempt-1, fmt.Sprintf("line %d", attempt-1))
			return
		}
		sendEnd(w, "passed")
	})

	lines, end, err := collect(context.Background(), client)
	if err != nil {
		t.Fatalf("Follow: %v", err)
	}

	if len(lines) != drops || end.Status != "passed" || int(state.hits.Load()) != drops+1 {
		t.Errorf("%d lines, end %+v, %d connections", len(lines), end, state.hits.Load())
	}
}

func TestAStreamThatNeverGetsAnywhereIsGivenUpOnAfterABoundedNumberOfTries(t *testing.T) {
	for name, script := range map[string]func(int, http.ResponseWriter, *http.Request){
		"connections that close at once": func(_ int, w http.ResponseWriter, _ *http.Request) { startStream(w) },
		"a server that fails":            func(_ int, w http.ResponseWriter, _ *http.Request) { writeJSON(w, 500, `{"error":"oops"}`) },
		"a server that is unavailable":   func(_ int, w http.ResponseWriter, _ *http.Request) { writeJSON(w, 503, `{"error":"try again"}`) },
		"a page that is not a stream": func(_ int, w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>captive portal</html>"))
		},
		"a connection that is cut": func(_ int, w http.ResponseWriter, _ *http.Request) {
			conn, _, _ := w.(http.Hijacker).Hijack()
			_ = conn.Close()
		},
	} {
		t.Run(name, func(t *testing.T) {
			client, state := streamer(t, script)

			lines, _, err := collect(context.Background(), client)

			if !errors.Is(err, ErrStreamGaveUp) {
				t.Fatalf("got %v, want ErrStreamGaveUp", err)
			}
			if len(lines) != 0 {
				t.Errorf("lines %v", messages(lines))
			}
			if n := int(state.hits.Load()); n != maxStreamFailures {
				t.Errorf("%d connections, want exactly %d", n, maxStreamFailures)
			}
		})
	}
}

// The pause between tries grows, so that a server in trouble is not met with a
// tool that hammers it, and it is capped, so that one in recovery is not waited
// on for minutes.
func TestThePauseBetweenTriesGrowsAndIsCapped(t *testing.T) {
	previous := time.Duration(0)
	for attempt := 1; attempt <= 12; attempt++ {
		pause := defaultStreamBackoff(attempt)
		if pause < previous {
			t.Errorf("attempt %d waits %s, less than the %s before it", attempt, pause, previous)
		}
		if pause <= 0 || pause > 10*time.Second {
			t.Errorf("attempt %d waits %s", attempt, pause)
		}
		previous = pause
	}
	if defaultStreamBackoff(1) >= defaultStreamBackoff(4) {
		t.Error("the pause does not grow")
	}
}

func TestTheClientWaitsBetweenTriesAndNotAfterTheLast(t *testing.T) {
	var asked []int
	client, state := streamer(t, func(_ int, w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 500, `{"error":"oops"}`)
	}, WithStreamBackoff(func(attempt int) time.Duration {
		asked = append(asked, attempt)
		return time.Millisecond
	}))

	_, _, err := collect(context.Background(), client)

	if !errors.Is(err, ErrStreamGaveUp) {
		t.Fatalf("got %v", err)
	}
	// One pause between each pair of connections: none before the first, none
	// after the last.
	if len(asked) != int(state.hits.Load())-1 {
		t.Errorf("%d pauses for %d connections", len(asked), state.hits.Load())
	}
}

// --- What is not tried again -------------------------------------------------------------------------------

func TestWhatNoRetryCanFixIsNotRetried(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			client, state := streamer(t, func(_ int, w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, status, `{"error":"no such run"}`)
			})

			_, _, err := collect(context.Background(), client)

			var answered *APIError
			if !errors.As(err, &answered) || answered.Status != status {
				t.Fatalf("got %v, want the API's answer %d", err, status)
			}
			if state.hits.Load() != 1 {
				t.Errorf("%d connections, want one: this answer does not change", state.hits.Load())
			}
			if errors.Is(err, ErrStreamGaveUp) {
				t.Error("a final answer is not a giving up")
			}
		})
	}
}

func TestARedirectIsFinalToo(t *testing.T) {
	client, state := streamer(t, func(_ int, w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://elsewhere.example.test/runs/r1/logs", http.StatusFound)
	})

	_, _, err := collect(context.Background(), client)

	var answered *APIError
	if !errors.As(err, &answered) || !strings.Contains(err.Error(), "redirect") {
		t.Errorf("got %v", err)
	}
	if state.hits.Load() != 1 {
		t.Errorf("%d connections", state.hits.Load())
	}
}

// The error event is the server saying the stream stopped — a right was withdrawn,
// or something broke after the answer had begun. It does not say which, and
// reconnecting would only be told the same.
func TestAStreamTheServerStoppedIsNotReconnected(t *testing.T) {
	client, state := streamer(t, func(_ int, w http.ResponseWriter, _ *http.Request) {
		startStream(w)
		sendLine(w, 0, "zero")
		_, _ = fmt.Fprint(w, "event: error\ndata: {\"error\":\"the log stream stopped\"}\n\n")
		w.(http.Flusher).Flush()
	})

	lines, _, err := collect(context.Background(), client)

	var stopped *StreamStopped
	if !errors.As(err, &stopped) {
		t.Fatalf("got %v, want the server's stop", err)
	}
	if stopped.Message != "the log stream stopped" {
		t.Errorf("message %q", stopped.Message)
	}
	if len(lines) != 1 || state.hits.Load() != 1 {
		t.Errorf("%d lines over %d connection(s)", len(lines), state.hits.Load())
	}
}

// --- Silence ------------------------------------------------------------------------------------------------------

// A connection can die without either side being told: a NAT forgets it, a laptop
// sleeps. The server sends a comment every fifteen seconds so that silence means
// something, and a stream that stays silent for longer than that is reconnected.
func TestASilentStreamIsReconnected(t *testing.T) {
	client, state := streamer(t, func(attempt int, w http.ResponseWriter, r *http.Request) {
		startStream(w)
		if attempt == 1 {
			sendLine(w, 0, "zero")
			<-r.Context().Done() // nothing more, ever, until the client gives up
			return
		}
		sendLine(w, 1, "one")
		sendEnd(w, "passed")
	}, WithStreamIdleTimeout(150*time.Millisecond))

	lines, end, err := collect(context.Background(), client)

	if err != nil {
		t.Fatalf("Follow: %v", err)
	}
	if got := strings.Join(messages(lines), ","); got != "zero,one" || end.Status != "passed" {
		t.Errorf("lines %q, end %+v", got, end)
	}
	if ids := state.lastEventIDs(); len(ids) != 2 || ids[1] != "0" {
		t.Errorf("Last-Event-ID %q, want a reconnection from line 0", ids)
	}
}

// What the server sends as a keep-alive is activity, and is not mistaken for
// silence.
func TestKeepAliveCommentsKeepAStreamOpen(t *testing.T) {
	client, state := streamer(t, func(_ int, w http.ResponseWriter, _ *http.Request) {
		startStream(w)
		for i := 0; i < 8; i++ {
			time.Sleep(50 * time.Millisecond)
			_, _ = fmt.Fprint(w, ": keep-alive\n\n")
			w.(http.Flusher).Flush()
		}
		sendLine(w, 0, "late")
		sendEnd(w, "passed")
	}, WithStreamIdleTimeout(200*time.Millisecond))

	lines, _, err := collect(context.Background(), client)

	if err != nil || len(lines) != 1 {
		t.Fatalf("lines %v, err %v", messages(lines), err)
	}
	if state.hits.Load() != 1 {
		t.Errorf("%d connections: a stream kept alive was reconnected", state.hits.Load())
	}
}

// --- Stopping ---------------------------------------------------------------------------------------------------------

func TestACancelledContextStopsTheStreamAndClosesTheConnection(t *testing.T) {
	closed := make(chan struct{})
	client, _ := streamer(t, func(_ int, w http.ResponseWriter, r *http.Request) {
		startStream(w)
		sendLine(w, 0, "zero")
		<-r.Context().Done()
		close(closed)
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	lines, _, err := collect(ctx, client)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want the context's cancellation", err)
	}
	if len(lines) != 1 {
		t.Errorf("lines %v", messages(lines))
	}
	if time.Since(start) > 2*time.Second {
		t.Errorf("the stream outlived its context by %s", time.Since(start))
	}
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Error("the connection was left open")
	}
}

func TestACancelledContextInterruptsThePauseBetweenTries(t *testing.T) {
	client, state := streamer(t, func(_ int, w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 500, `{"error":"oops"}`)
	}, WithStreamBackoff(func(int) time.Duration { return time.Hour }))

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, _, err := collect(ctx, client)

	if !errors.Is(err, context.Canceled) || time.Since(start) > 2*time.Second {
		t.Errorf("got %v after %s", err, time.Since(start))
	}
	if state.hits.Load() != 1 {
		t.Errorf("%d connections", state.hits.Load())
	}
}

// --- What is printed ------------------------------------------------------------------------------------------------------

// A line is somebody's program output and is passed on as it is, down to the
// indentation of a k6 summary. What is taken out is what would make a log lie.
func TestALineIsPassedOnWithItsIndentationAndWithoutWhatWouldRewriteALog(t *testing.T) {
	client, _ := streamer(t, func(_ int, w http.ResponseWriter, _ *http.Request) {
		startStream(w)
		sendLine(w, 0, "     http_req_duration..: avg=120ms   p(95)=480ms")
		sendLine(w, 1, "\tindented with a tab")
		sendLine(w, 2, "before\x1b[2Jafter")
		sendLine(w, 3, "first\r\nsecond")
		sendLine(w, 4, "mark\u202eevil")
		sendEnd(w, "passed")
	})

	lines, _, err := collect(context.Background(), client)
	if err != nil {
		t.Fatalf("Follow: %v", err)
	}

	if lines[0].Message != "     http_req_duration..: avg=120ms   p(95)=480ms" {
		t.Errorf("the indentation of a summary was changed: %q", lines[0].Message)
	}
	if lines[1].Message != "\tindented with a tab" {
		t.Errorf("a tab was changed: %q", lines[1].Message)
	}
	for _, line := range lines[2:] {
		if strings.ContainsAny(line.Message, "\x1b\r\n\u202e") {
			t.Errorf("a control character reached the output: %q", line.Message)
		}
	}
}

func TestALongLineIsPassedOnWhole(t *testing.T) {
	long := strings.Repeat("x", 200_000)
	client, _ := streamer(t, func(_ int, w http.ResponseWriter, _ *http.Request) {
		startStream(w)
		sendLine(w, 0, long)
		sendEnd(w, "passed")
	})

	lines, _, err := collect(context.Background(), client)

	if err != nil || len(lines) != 1 || lines[0].Message != long {
		t.Errorf("err %v, %d line(s)", err, len(lines))
	}
}

func TestTheTokenIsNeverInALineOrInAnErrorOfTheStream(t *testing.T) {
	t.Run("in a line", func(t *testing.T) {
		client, _ := streamer(t, func(_ int, w http.ResponseWriter, _ *http.Request) {
			startStream(w)
			sendLine(w, 0, "the token is "+aToken+" and that is all")
			sendLine(w, 1, "Authorization: Bearer "+aToken)
			sendEnd(w, "passed")
		})

		lines, _, err := collect(context.Background(), client)
		if err != nil {
			t.Fatalf("Follow: %v", err)
		}
		for _, line := range lines {
			if strings.Contains(line.Message, aToken) || strings.Contains(line.Message, aToken[4:]) {
				t.Errorf("the token is in a line: %q", line.Message)
			}
		}
	})

	t.Run("in the server's stop", func(t *testing.T) {
		client, _ := streamer(t, func(_ int, w http.ResponseWriter, _ *http.Request) {
			startStream(w)
			_, _ = fmt.Fprintf(w, "event: error\ndata: {\"error\":\"rejected %s\"}\n\n", aToken)
			w.(http.Flusher).Flush()
		})

		_, _, err := collect(context.Background(), client)

		for _, shown := range []string{fmt.Sprint(err), fmt.Sprintf("%+v|%#v", err, err)} {
			if err == nil || strings.Contains(shown, aToken) || strings.Contains(shown, aToken[4:]) {
				t.Errorf("the token is in an error: %s", shown)
			}
		}
	})

	t.Run("in an answer that refuses", func(t *testing.T) {
		client, _ := streamer(t, func(_ int, w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, 401, `{"error":"bad token `+aToken+`"}`)
		})

		_, _, err := collect(context.Background(), client)

		if err == nil || strings.Contains(err.Error(), aToken) || strings.Contains(err.Error(), aToken[4:]) {
			t.Errorf("got %v", err)
		}
	})
}

func TestAnIdentifierThatIsNotOneIsNotFollowed(t *testing.T) {
	client, state := streamer(t, func(_ int, w http.ResponseWriter, _ *http.Request) { startStream(w) })

	_, err := client.Follow(context.Background(), "../admin", func(LogLine) {})

	if err == nil || state.hits.Load() != 0 {
		t.Errorf("got %v after %d connection(s)", err, state.hits.Load())
	}
}

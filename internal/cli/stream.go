package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	// maxStreamFailures is how many connections in a row may fail to get anywhere
	// before the stream is given up on. A connection gets somewhere when it brings
	// at least one new line; one that does is not a failure of the next.
	maxStreamFailures = 5

	// defaultStreamIdleTimeout is three of the server's keep-alive intervals. The
	// server writes a comment every fifteen seconds down a stream with nothing to
	// say, so a connection that has said nothing at all for this long is dead
	// without either side having been told.
	defaultStreamIdleTimeout = 45 * time.Second

	// maxEventLineBytes bounds one line of the stream. A run's line is a line of
	// somebody's program output, and a long one is not unusual; a megabyte is
	// past anything a program writes on purpose.
	maxEventLineBytes = 1 << 20
)

// ErrStreamGaveUp means the log stream could not be kept open. It says nothing
// about the run: the verdict is read from the run itself.
var ErrStreamGaveUp = errors.New("the log stream could not be kept open")

// StreamStopped is the server ending the stream with an error event. It does not
// say why — a right withdrawn and a fault after the answer began read the same —
// and asking again would be told the same.
type StreamStopped struct{ Message string }

func (e *StreamStopped) Error() string {
	return "the server stopped the log stream: " + e.Message
}

// LogLine is one line of a run's output.
type LogLine struct {
	// Seq is the line's number in the run, from zero. It is what a reconnection
	// resumes from.
	Seq     int64     `json:"seq"`
	Stream  string    `json:"stream"`
	Message string    `json:"message"`
	At      time.Time `json:"at"`
}

// StreamEnd is the end event: the run is over, and this is how.
type StreamEnd struct{ Status string }

// errDropped is a connection that closed without the end event. It means "try
// again", which a closed connection says to every event stream there is.
var errDropped = errors.New("the connection closed before the run ended")

// defaultStreamBackoff is the pause before the nth reconnection: a second, then
// two, then four, capped at eight. A server in trouble is not met with a tool
// that hammers it, and one in recovery is not waited on for minutes.
func defaultStreamBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 4 {
		attempt = 4
	}
	return time.Second << (attempt - 1)
}

// Follow reads a run's output, calling emit once for each line, in order, until
// the run ends.
//
// A connection that drops is reopened with the number of the last line read, and
// the server answers with what follows: no line twice and none skipped. A line the
// server sends again anyway is not passed on again. What cannot be fixed by trying
// again is not tried again — an answer that refuses, a redirect, the server's own
// stop — and what can is tried a bounded number of times in a row, with a pause
// that grows.
//
// The stream is a convenience and not the verdict. A failure of it is returned
// and the caller keeps waiting for the run, which it reads from the run itself.
func (c *Client) Follow(ctx context.Context, runID string, emit func(LogLine)) (StreamEnd, error) {
	if !runIDShape.MatchString(runID) {
		return StreamEnd{}, errors.New("an identifier from Ballast is not one: refusing to put it in an address")
	}

	last := int64(-1)
	failures := 0

	for {
		end, progressed, err := c.followOnce(ctx, runID, &last, emit)
		if err == nil {
			return end, nil
		}
		if ctx.Err() != nil {
			return StreamEnd{}, ctx.Err()
		}

		var stopped *StreamStopped
		var refusal *APIError
		switch {
		case errors.As(err, &stopped):
			return StreamEnd{}, err
		case errors.As(err, &refusal) && !worthRetrying(refusal.Status):
			return StreamEnd{}, err
		}

		if progressed {
			failures = 0
		}
		failures++
		if failures >= maxStreamFailures {
			return StreamEnd{}, fmt.Errorf("%w after %d attempts in a row (%s)", ErrStreamGaveUp, failures, c.clean(err.Error()))
		}

		select {
		case <-time.After(c.streamBackoff(failures)):
		case <-ctx.Done():
			return StreamEnd{}, ctx.Err()
		}
	}
}

// worthRetrying says whether another try could be answered differently. A server
// that is failing, or asking to be given a moment, might be; one that says the
// token is not good or the run is not there will say it again.
func worthRetrying(status int) bool {
	return status >= 500 || status == http.StatusTooManyRequests || status == http.StatusRequestTimeout
}

// followOnce is one connection. progressed says it brought a line the caller had
// not seen.
func (c *Client) followOnce(
	ctx context.Context, runID string, last *int64, emit func(LogLine),
) (end StreamEnd, progressed bool, err error) {
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	// The idle timer cancels this connection and nothing else.
	idle := time.AfterFunc(c.streamIdle, cancel)
	defer idle.Stop()

	request, err := http.NewRequestWithContext(streamCtx, http.MethodGet, c.base.JoinPath("runs", runID, "logs").String(), nil)
	if err != nil {
		return StreamEnd{}, false, errors.New("the request could not be built")
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set("Cache-Control", "no-cache")
	request.Header.Set("User-Agent", userAgent)
	if *last >= 0 {
		request.Header.Set("Last-Event-ID", strconv.FormatInt(*last, 10))
	}

	response, err := c.http.Do(request)
	if err != nil {
		return StreamEnd{}, false, c.streamTransportError(ctx, err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		return StreamEnd{}, false, c.refusal(response.StatusCode, response.Header, body)
	}
	if !strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
		return StreamEnd{}, false, errors.New("the answer is not an event stream")
	}

	reader := bufio.NewReaderSize(&activityReader{r: response.Body, onRead: func() { idle.Reset(c.streamIdle) }}, 64<<10)
	end, progressed, err = c.readEvents(reader, last, emit)
	if err != nil && ctx.Err() == nil && streamCtx.Err() != nil {
		// Cancelled by the idle timer: the connection said nothing for too long.
		err = fmt.Errorf("nothing was received for %s", c.streamIdle)
	}
	return end, progressed, err
}

func (c *Client) streamTransportError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	inner := err
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		inner = urlErr.Err
	}
	return fmt.Errorf("could not reach %s: %s", c.base.Host, c.clean(inner.Error()))
}

// activityReader tells whoever is waiting that something arrived.
type activityReader struct {
	r      io.Reader
	onRead func()
}

func (a *activityReader) Read(p []byte) (int, error) {
	n, err := a.r.Read(p)
	if n > 0 {
		a.onRead()
	}
	return n, err
}

// readEvents reads server-sent events until the run ends or the connection does.
//
// It reads what the protocol allows and not only what this server happens to send:
// both line endings, a field with no space after its colon, a data field in
// several lines, comments, fields and events it does not know.
func (c *Client) readEvents(
	reader *bufio.Reader, last *int64, emit func(LogLine),
) (end StreamEnd, progressed bool, err error) {
	var (
		name  string
		data  []string
		first = true
	)

	for {
		line, err := readEventLine(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return StreamEnd{}, progressed, errDropped
			}
			return StreamEnd{}, progressed, err
		}
		if first {
			line, first = strings.TrimPrefix(line, "\xef\xbb\xbf"), false
		}

		switch {
		case line == "":
			event, payload := name, strings.Join(data, "\n")
			name, data = "", nil

			switch event {
			case "line":
				var parsed LogLine
				if json.Unmarshal([]byte(payload), &parsed) != nil || parsed.Seq <= *last {
					// Not a line this tool can read, or one it has already printed.
					continue
				}
				parsed.Stream = c.clean(parsed.Stream)
				parsed.Message = c.cleanLine(parsed.Message)
				*last = parsed.Seq
				progressed = true
				emit(parsed)

			case "end":
				var parsed struct {
					Status string `json:"status"`
				}
				_ = json.Unmarshal([]byte(payload), &parsed)
				return StreamEnd{Status: c.clean(parsed.Status)}, progressed, nil

			case "error":
				var parsed struct {
					Error string `json:"error"`
				}
				message := "no reason was given"
				if json.Unmarshal([]byte(payload), &parsed) == nil && strings.TrimSpace(parsed.Error) != "" {
					message = c.clean(parsed.Error)
				}
				return StreamEnd{}, progressed, &StreamStopped{Message: message}
			}

		case strings.HasPrefix(line, ":"):
			// A comment: the server's keep-alive, or a proxy's.

		default:
			field, value, _ := strings.Cut(line, ":")
			value = strings.TrimPrefix(value, " ")
			switch field {
			case "event":
				name = value
			case "data":
				data = append(data, value)
			}
			// id, retry and anything else are not needed: the number of a line
			// is in its data, and the pause between tries is the tool's own.
		}
	}
}

// readEventLine reads one line of the stream, of either ending, up to a bound.
func readEventLine(reader *bufio.Reader) (string, error) {
	var line []byte
	for {
		part, isPrefix, err := reader.ReadLine()
		if err != nil {
			return "", err
		}
		line = append(line, part...)
		if len(line) > maxEventLineBytes {
			return "", errors.New("a line of the stream is longer than this tool will read")
		}
		if !isPrefix {
			return string(line), nil
		}
	}
}

// cleanLine makes a line of a run's output safe to print into a log, and nothing
// more. It is somebody's program output and goes through as it is — indentation
// and tabs included, a k6 summary is laid out with them — except for what would
// make a log lie: an escape sequence that rewrites a terminal, a line break that
// forges another line, a mark that makes text read backwards. The token goes too,
// wherever it is.
func (c *Client) cleanLine(line string) string {
	line = strings.Map(func(r rune) rune {
		switch {
		case r == '\t':
			return r
		case r == 0x1b:
			return -1
		case unicode.IsControl(r), isInvisible(r):
			return ' '
		}
		return r
	}, line)
	return c.redactor.Redact(line)
}

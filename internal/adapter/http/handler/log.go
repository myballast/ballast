package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/adapter/http/dto"
	"github.com/youssefmahersi/ballast/internal/usecase/run"
)

const (
	// logPollInterval is how often the stream looks for lines that arrived
	// since the last read. The worker files a batch every half second, so a
	// line reaches a screen about a second and a half after it was written.
	logPollInterval = time.Second

	// keepAliveInterval sends a comment down an idle stream. A run that prints
	// nothing for a minute is ordinary; a proxy that drops a connection with no
	// traffic on it is ordinary too, and the comment is what keeps the two from
	// meeting.
	keepAliveInterval = 15 * time.Second

	// beforeTheFirstLine is where a stream starts when the client does not say
	// otherwise. Sequence numbers start at zero, so "after -1" is the whole
	// run — which is what somebody opening the page mid-run has to be given.
	beforeTheFirstLine = -1
)

// Logs handles GET /runs/{id}/logs, as Server-Sent Events.
//
// Every event carries its line's sequence number as the event id. That is the
// whole of the resumption: a browser that loses the connection reconnects with
// Last-Event-ID set to the last id it received, and the stream answers with
// what follows — no line twice, none skipped. Nothing is kept in memory
// between connections, because nothing has to be: the sequence number in the
// table is the state.
//
// The stream ends when the run does, with an event saying how. A client that
// sees it knows not to reconnect, which an ordinary close does not say.
func (h *Run) Logs(w http.ResponseWriter, r *http.Request) {
	who, ok := signedIn(w, r)
	if !ok {
		return
	}

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "the run id is not a uuid")
		return
	}

	// Without flushing there is no stream: the whole response would arrive when
	// the run ended, which is the one thing this endpoint is not for. Said
	// loudly rather than served quietly as a very slow request.
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.log.ErrorContext(r.Context(), "the response writer cannot flush; a log stream is impossible",
			"path", r.URL.Path)
		writeError(w, http.StatusInternalServerError, "something went wrong")
		return
	}

	after := resumeFrom(r)

	// The first read happens before a single header is written, so that a run
	// the caller may not see is answered with a JSON 404 rather than with an
	// empty event stream — the client is a browser, and 404 is what its
	// EventSource needs to stop retrying.
	lines, status, err := h.runs.LogsSince(r.Context(), who, id, after, run.MaxLogPage)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Reverse proxies buffer a response by default, which turns a live stream
	// into one long silence followed by everything at once.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	polling := time.NewTicker(logPollInterval)
	defer polling.Stop()
	idle := time.NewTicker(keepAliveInterval)
	defer idle.Stop()

	for {
		for _, line := range lines {
			if err := writeEvent(w, "line", strconv.FormatInt(line.Seq, 10), dto.LogLine{
				Seq:     line.Seq,
				Stream:  line.Stream,
				Message: line.Message,
				At:      line.At,
			}); err != nil {
				// The client is gone. Nothing to report: it is how a browser
				// closes a stream it no longer wants.
				return
			}
			after = line.Seq
		}

		if len(lines) > 0 {
			flusher.Flush()
			idle.Reset(keepAliveInterval)
		}

		// A page that came back full means there is more waiting, so the next
		// read happens at once rather than a second from now: a viewer opening
		// a finished run's page should not watch it fill up at 500 lines a
		// second.
		if len(lines) < run.MaxLogPage {
			if status.IsTerminal() {
				// The run is over and nothing is left to send. Said in an
				// event, because a closed connection on its own means "try
				// again" to every SSE client there is.
				_ = writeEvent(w, "end", "", dto.RunEnded{Status: string(status)})
				flusher.Flush()
				return
			}

			select {
			case <-r.Context().Done():
				return
			case <-idle.C:
				// A comment: ignored by the client, enough to keep whatever
				// sits in between from deciding the connection is dead.
				if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
					return
				}
				flusher.Flush()
			case <-polling.C:
			}
		}

		lines, status, err = h.runs.LogsSince(r.Context(), who, id, after, run.MaxLogPage)
		if err != nil {
			if r.Context().Err() != nil {
				// The client disconnected while this read was in flight.
				return
			}

			// Mid-stream, and 200 has already been sent: the only honest thing
			// left is to say so in an event and close. A permission removed
			// while somebody was watching arrives here too, which is why the
			// message says nothing about which it was.
			h.log.ErrorContext(r.Context(), "a log stream stopped",
				"run_id", id, "error", err)
			_ = writeEvent(w, "error", "", dto.Error{Error: "the log stream stopped"})
			flusher.Flush()
			return
		}
	}
}

// writeEvent writes one Server-Sent Event: an optional id, a name, and one line
// of JSON.
//
// The payload is JSON precisely because the protocol is line-based: a message
// containing a newline would otherwise end the event early, and a generator's
// output is somebody else's text. JSON escapes it, so one line in means one
// line out.
func writeEvent(w http.ResponseWriter, name, id string, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	if id != "" {
		if _, err := fmt.Fprintf(w, "id: %s\n", id); err != nil {
			return err
		}
	}

	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, encoded)
	return err
}

// resumeFrom reads where a client wants the stream to start.
//
// Last-Event-ID is the browser's own doing: EventSource sends it on every
// reconnection, with the id of the last event it received. Absent — the first
// connection — the answer is the whole run from its first line, which is what
// makes arriving in the middle of a run show everything that has happened.
//
// An unreadable value is treated as absent rather than refused. The alternative
// is a client stuck in a reconnection loop over a header it did not write by
// hand, and showing a run from the beginning is never wrong, only repetitive.
func resumeFrom(r *http.Request) int64 {
	raw := r.Header.Get("Last-Event-ID")
	if raw == "" {
		return beforeTheFirstLine
	}

	seq, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || seq < 0 {
		return beforeTheFirstLine
	}

	return seq
}

//go:build integration

// The live log view, from the generator's first line to the browser's second
// connection.
//
// The ticket's two acceptance criteria are both about what a person sees, so
// they are read here through the API's own stream rather than from the table:
// somebody opening the page while a run is under way, and somebody whose
// connection dropped.
package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/adapter/postgres"
	"github.com/youssefmahersi/ballast/internal/domain"
)

// loggingScript prints three things: an ordinary line, the credential it was
// given, and a header carrying it. A script is code somebody uploaded, and
// nothing stops it doing exactly this.
const loggingScript = `
import http from 'k6/http';

export default function () {
	console.log('BALLAST_ORDINARY calling the target');
	console.log('BALLAST_LEAK token=' + __ENV.API_TOKEN);
	console.log('Authorization: Bearer ' + __ENV.API_TOKEN);
	http.get(` + "`${__ENV.BASE_URL}/healthz`" + `);
}
`

// event is one Server-Sent Event as a client sees it.
type event struct {
	id   string
	name string
	data string
}

// seq is the line number an event carries, read from its data rather than from
// its id: a client that trusted only the id would not notice the two drifting
// apart, and the resumption depends on them being the same number.
func (e event) seq(t *testing.T) int64 {
	t.Helper()

	var line struct {
		Seq     int64  `json:"seq"`
		Stream  string `json:"stream"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(e.data), &line); err != nil {
		t.Fatalf("decode %q: %v", e.data, err)
	}

	if e.id != strconv.FormatInt(line.Seq, 10) {
		t.Fatalf("the event id is %q and its line is numbered %d", e.id, line.Seq)
	}
	return line.Seq
}

func (e event) message(t *testing.T) string {
	t.Helper()

	var line struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(e.data), &line); err != nil {
		t.Fatalf("decode %q: %v", e.data, err)
	}
	return line.Message
}

// follow opens the log stream, as a browser's EventSource would. lastEventID is
// what a reconnecting browser sends; empty means a first connection.
func (s stack) follow(ctx context.Context, t *testing.T, id, lastEventID string) *http.Response {
	t.Helper()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.api.URL+"/runs/"+id+"/logs", nil)
	if err != nil {
		t.Fatalf("build the request: %v", err)
	}
	req.AddCookie(s.session)
	req.Header.Set("Accept", "text/event-stream")
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}

	res, err := s.api.Client().Do(req)
	if err != nil {
		t.Fatalf("open the stream: %v", err)
	}
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()
		t.Fatalf("GET /runs/%s/logs returned %d: %s", id, res.StatusCode, body)
	}
	if kind := res.Header.Get("Content-Type"); !strings.HasPrefix(kind, "text/event-stream") {
		t.Fatalf("the stream is %q", kind)
	}

	return res
}

// drainStream reads events until the run's end event arrives, and returns the
// line events in the order they were sent.
//
// Comments are skipped, which is what a client does with them: they exist to
// keep whatever sits in between from deciding an idle connection is dead.
func drainStream(t *testing.T, body io.Reader) []event {
	t.Helper()

	var (
		lines   []event
		current event
		scanner = bufio.NewScanner(body)
	)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)

	for scanner.Scan() {
		text := scanner.Text()

		switch {
		case strings.HasPrefix(text, ":"):
			// A keep-alive comment.

		case strings.HasPrefix(text, "id: "):
			current.id = strings.TrimPrefix(text, "id: ")

		case strings.HasPrefix(text, "event: "):
			current.name = strings.TrimPrefix(text, "event: ")

		case strings.HasPrefix(text, "data: "):
			current.data = strings.TrimPrefix(text, "data: ")

		case text == "":
			switch current.name {
			case "line":
				lines = append(lines, current)
			case "end":
				return lines
			case "error":
				t.Fatalf("the stream reported an error: %s", current.data)
			}
			current = event{}
		}
	}

	if err := scanner.Err(); err != nil {
		t.Fatalf("read the stream: %v", err)
	}

	t.Fatal("the stream closed without saying the run had ended")
	return nil
}

// runThatLogs seeds a project whose scenario prints, with a credential to
// print, and queues a short run of it.
func runThatLogs(ctx context.Context, t *testing.T, db *postgres.DB, token string) (stack, string) {
	t.Helper()

	target := newTarget(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	org, scenario, environment := seed(ctx, t, db, target, nil)

	if err := db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE scenario SET script = $3 WHERE org_id = $1 AND id = $2`,
			org, scenario, loggingScript)
		return err
	}); err != nil {
		t.Fatalf("point the scenario at the printing script: %v", err)
	}

	if _, err := postgres.NewSecretRepository(db, cipher(t)).
		Put(ctx, org, environment, "API_TOKEN", token, time.Now().UTC()); err != nil {
		t.Fatalf("store the secret: %v", err)
	}

	s := build(t, db, org)

	status, queued := s.enqueue(t, fmt.Sprintf(
		`{"scenario_id":%q,"environment_id":%q,"profile":"smoke","params":{"target_vus":1,"plateau_s":3}}`,
		scenario, environment))
	if status != http.StatusAccepted {
		t.Fatalf("POST /runs returned %d", status)
	}

	return s, queued.ID
}

// The first acceptance criterion, and the censorship in the same run: somebody
// opening the page while the generator is working sees the output from its very
// first line, and what they see never contains the credential the script
// printed.
func TestAViewerArrivingMidRunSeesEverythingFromTheBeginning(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	const token = "ghp_a_token_a_script_decided_to_print"

	db, err := postgres.Open(ctx, dsn())
	if err != nil {
		t.Fatalf("open the database: %v", err)
	}
	t.Cleanup(db.Close)

	s, runID := runThatLogs(ctx, t, db, token)

	executing := s.executing(ctx, t)

	// Arriving in the middle: the run is already under way, and lines have
	// already been written and stored.
	s.waitForStatus(ctx, t, runID, 4*time.Minute, string(domain.StatusRunning))
	waitForLines(ctx, t, db, s.org, mustUUID(t, runID))

	res := s.follow(ctx, t, runID, "")
	defer func() { _ = res.Body.Close() }()

	lines := drainStream(t, res.Body)
	if len(lines) == 0 {
		t.Fatal("the stream carried no output at all")
	}

	// From the beginning: the first event is the run's first line, even though
	// the stream was opened long after it was written.
	if first := lines[0].seq(t); first != 0 {
		t.Fatalf("the stream started at line %d", first)
	}

	// Strictly increasing, which is both halves of being usable: no line twice
	// and none out of order.
	for i := 1; i < len(lines); i++ {
		if previous, current := lines[i-1].seq(t), lines[i].seq(t); current <= previous {
			t.Fatalf("line %d followed line %d", current, previous)
		}
	}

	// What the script printed, as the viewer sees it.
	var sawOrdinary, sawRedaction bool
	for _, line := range lines {
		message := line.message(t)

		if strings.Contains(message, token) {
			t.Fatalf("the credential reached the viewer: %q", message)
		}
		if strings.Contains(message, "BALLAST_ORDINARY") {
			sawOrdinary = true
		}
		if strings.Contains(message, domain.Redacted) {
			sawRedaction = true
		}
	}
	if !sawOrdinary {
		t.Error("the script's ordinary output never arrived, so this proves little about the rest")
	}
	if !sawRedaction {
		t.Error("nothing was redacted, so the lines that carried the credential never arrived")
	}

	select {
	case <-executing:
	case <-ctx.Done():
		t.Fatalf("the worker never returned: %v", ctx.Err())
	}
}

// The second acceptance criterion. A browser whose connection dropped
// reconnects on its own, with the id of the last event it received; it must be
// given what follows and nothing it already has.
//
// The lines are written through the repository rather than by a generator: what
// is under test is the endpoint's resumption, and a container printing on cue
// is a slow and imprecise way to ask about it. That the worker stores what a
// generator writes is the test above.
func TestAReconnectionResumesWithoutRepeatingALine(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	target := newTarget(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	db, err := postgres.Open(ctx, dsn())
	if err != nil {
		t.Fatalf("open the database: %v", err)
	}
	t.Cleanup(db.Close)

	org, scenario, environment := seed(ctx, t, db, target, nil)
	s := build(t, db, org)

	_, queued := s.enqueue(t, fmt.Sprintf(
		`{"scenario_id":%q,"environment_id":%q,"profile":"smoke","params":{"target_vus":1,"plateau_s":3}}`,
		scenario, environment))

	// A run that is over, with output: the stream then sends everything it has
	// and ends, which is what makes the two connections comparable.
	written := []domain.LogLine{}
	at := time.Now().UTC().Truncate(time.Microsecond)
	for i := range 6 {
		written = append(written, domain.LogLine{
			Seq:     int64(i),
			Stream:  domain.StreamStdout,
			Message: fmt.Sprintf("line number %d", i),
			At:      at.Add(time.Duration(i) * time.Millisecond),
		})
	}
	runID := mustUUID(t, queued.ID)
	if err := postgres.NewLogRepository(db).Append(ctx, org, runID, written); err != nil {
		t.Fatalf("store the lines: %v", err)
	}
	if _, err := postgres.NewRunRepository(db).Cancel(ctx, org, runID, at, "no worker in this test"); err != nil {
		t.Fatalf("end the run: %v", err)
	}

	// The first connection, which sees everything.
	first := s.follow(ctx, t, queued.ID, "")
	everything := drainStream(t, first.Body)
	_ = first.Body.Close()

	if len(everything) != len(written) {
		t.Fatalf("the first connection saw %d line(s) of %d", len(everything), len(written))
	}

	// The connection drops after line 2, and the browser reconnects saying so.
	resumed := s.follow(ctx, t, queued.ID, "2")
	defer func() { _ = resumed.Body.Close() }()

	rest := drainStream(t, resumed.Body)
	if len(rest) != 3 {
		t.Fatalf("resuming after line 2 sent %d line(s), wanted 3", len(rest))
	}
	if first := rest[0].seq(t); first != 3 {
		t.Fatalf("resuming after line 2 started at line %d", first)
	}

	// Not one line the client already held.
	for _, line := range rest {
		if seq := line.seq(t); seq <= 2 {
			t.Fatalf("line %d was sent twice", seq)
		}
	}
}

// waitForLines waits until the worker has stored some of the run's output, so
// that "arriving in the middle" means what it says.
func waitForLines(ctx context.Context, t *testing.T, db *postgres.DB, org, runID uuid.UUID) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		var stored int
		if err := db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx,
				`SELECT count(*) FROM run_log WHERE org_id = $1 AND run_id = $2`,
				org, runID).Scan(&stored)
		}); err != nil {
			t.Fatalf("count the stored lines: %v", err)
		}
		if stored > 0 {
			return
		}

		select {
		case <-ctx.Done():
			t.Fatalf("waiting for the first line: %v", ctx.Err())
		case <-time.After(200 * time.Millisecond):
		}
	}

	t.Fatal("the worker stored no line at all")
}

// mustUUID parses an identifier the API handed back.
func mustUUID(t *testing.T, raw string) uuid.UUID {
	t.Helper()

	parsed, err := uuid.Parse(raw)
	if err != nil {
		t.Fatalf("the API returned %q, which is not a uuid: %v", raw, err)
	}
	return parsed
}

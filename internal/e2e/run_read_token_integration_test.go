//go:build integration

package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// A pipeline that launched a run waits for it and reads how it went, with the
// token it launched it with. What these tests hold in place, end to end:
//
//   - the token reads its run — status, the word for the thresholds, the address
//     of the page — and follows its output;
//   - a token of another project is told the same thing as somebody without
//     rights and as somebody asking about a run that does not exist: byte for
//     byte, so nothing confirms that the identifier stands for something;
//   - revoking a token ends a stream that is open, within a second of the next
//     poll, and what the stream says does not say why.
//
// No container is started: the run stays queued, which is what keeps its stream
// open.

// aRunOfB is a run in project B, queued by somebody who is an administrator there
// for as long as it takes, so that the only way to B in these tests is the one
// under test.
func (w ciWorld) aRunOfB(ctx context.Context, t *testing.T) string {
	t.Helper()

	grant := func(sql string) {
		t.Helper()
		if err := w.db.InTenantTx(ctx, w.org, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, sql, w.org, w.adminID, w.b.projectID)
			return err
		}); err != nil {
			t.Fatalf("change the administrator's membership in B: %v", err)
		}
	}

	grant(`INSERT INTO membership (org_id, user_id, project_id, role) VALUES ($1, $2, $3, 'admin')`)
	res, raw := w.call(t, w.asAdmin(), http.MethodPost, "/runs", w.runBody(w.b))
	grant(`DELETE FROM membership WHERE org_id = $1 AND user_id = $2 AND project_id = $3`)

	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("queue a run in B: %d: %s", res.StatusCode, raw)
	}
	var queued struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &queued); err != nil {
		t.Fatalf("read the queued run: %v: %s", err, raw)
	}
	return queued.ID
}

// queueAs queues a run in A with a token and returns its id.
func (w ciWorld) queueAs(t *testing.T, secret string) string {
	t.Helper()

	res, raw := w.call(t, asPipeline(secret), http.MethodPost, "/runs", w.runBody(w.a))
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("queue a run: %d: %s", res.StatusCode, raw)
	}
	var queued struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &queued); err != nil {
		t.Fatalf("read the queued run: %v: %s", err, raw)
	}
	return queued.ID
}

// openStream opens a run's log stream as a pipeline would and returns the
// response once the server has answered; the caller owns its body.
func (w ciWorld) openStream(ctx context.Context, t *testing.T, secret, id string) *http.Response {
	t.Helper()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, w.api.URL+"/runs/"+id+"/logs", nil)
	if err != nil {
		t.Fatalf("build the request: %v", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	asPipeline(secret)(req)

	res, err := w.api.Client().Do(req)
	if err != nil {
		t.Fatalf("open the stream: %v", err)
	}
	return res
}

func TestAPipelineReadsItsRunAndFollowsItsOutput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newCIWorld(ctx, t)
	token := w.newToken(t, "ci-main")
	id := w.queueAs(t, token.secret)

	res, raw := w.call(t, asPipeline(token.secret), http.MethodGet, "/runs/"+id, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("read the run with its token: %d: %s", res.StatusCode, raw)
	}

	var got struct {
		Status      string `json:"status"`
		Verdict     string `json:"verdict"`
		URL         string `json:"url"`
		TriggeredBy string `json:"triggered_by"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("read the run: %v: %s", err, raw)
	}
	if got.Status != "queued" {
		t.Errorf("status %q, want queued", got.Status)
	}
	// Nothing has been judged yet, and the word says so rather than "met".
	if got.Verdict != "none" {
		t.Errorf("verdict %q, want none for a run that has produced nothing", got.Verdict)
	}
	if want := "http://web.test/runs/" + id; got.URL != want {
		t.Errorf("url %q, want %q", got.URL, want)
	}
	if got.TriggeredBy != "API token ci-main" {
		t.Errorf("triggered_by %q, want the token's name", got.TriggeredBy)
	}

	stream := w.openStream(ctx, t, token.secret, id)
	defer func() { _ = stream.Body.Close() }()
	if stream.StatusCode != http.StatusOK || !strings.HasPrefix(stream.Header.Get("Content-Type"), "text/event-stream") {
		t.Errorf("the stream: %d %q, want 200 and an event stream", stream.StatusCode, stream.Header.Get("Content-Type"))
	}
}

func TestATokenOfAnotherProjectIsToldTheSameAsSomebodyWithoutRights(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newCIWorld(ctx, t)
	token := w.newToken(t, "ci-main")
	inB := w.aRunOfB(ctx, t)
	nowhere := uuid.NewString()

	for _, route := range []string{"/runs/%s", "/runs/%s/logs"} {
		path := func(id string) string { return strings.Replace(route, "%s", id, 1) }

		// The token of A, asking about a run of B.
		byToken, tokenBody := w.call(t, asPipeline(token.secret), http.MethodGet, path(inB), "")
		// The administrator of A, who holds nothing in B.
		byPerson, personBody := w.call(t, w.asAdmin(), http.MethodGet, path(inB), "")
		// The same token, asking about a run that does not exist.
		unknown, unknownBody := w.call(t, asPipeline(token.secret), http.MethodGet, path(nowhere), "")

		for name, status := range map[string]int{"token": byToken.StatusCode, "person": byPerson.StatusCode, "unknown": unknown.StatusCode} {
			if status != http.StatusNotFound {
				t.Errorf("%s %s: %d, want 404", route, name, status)
			}
		}
		if string(tokenBody) != string(personBody) || string(tokenBody) != string(unknownBody) {
			t.Errorf("%s: the three answers differ:\n token:   %s\n person:  %s\n unknown: %s",
				route, tokenBody, personBody, unknownBody)
		}
	}
}

func TestRevokingATokenEndsAnOpenLogStreamWithinASecond(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newCIWorld(ctx, t)
	token := w.newToken(t, "ci-main")
	id := w.queueAs(t, token.secret)

	stream := w.openStream(ctx, t, token.secret, id)
	defer func() { _ = stream.Body.Close() }()
	if stream.StatusCode != http.StatusOK {
		t.Fatalf("open the stream: %d", stream.StatusCode)
	}

	type ending struct {
		at    time.Time
		event string
		data  string
	}
	ended := make(chan ending, 1)
	go func() {
		scanner := bufio.NewScanner(stream.Body)
		var name, data string
		for scanner.Scan() {
			text := scanner.Text()
			switch {
			case strings.HasPrefix(text, "event: "):
				name = strings.TrimPrefix(text, "event: ")
			case strings.HasPrefix(text, "data: "):
				data = strings.TrimPrefix(text, "data: ")
			case text == "" && name != "":
				if name == "error" || name == "end" {
					ended <- ending{at: time.Now(), event: name, data: data}
					return
				}
				name, data = "", ""
			}
		}
		ended <- ending{at: time.Now(), event: "closed"}
	}()

	// The stream survives several polls while the token is good. Without this a
	// stream that dies on its own would pass for one cut by the revocation.
	select {
	case e := <-ended:
		t.Fatalf("the stream ended by itself before any revocation: %s %s", e.event, e.data)
	case <-time.After(2500 * time.Millisecond):
	}

	revokedAt := time.Now()
	res, raw := w.call(t, w.asAdmin(), http.MethodDelete,
		"/projects/"+w.a.projectID.String()+"/api-tokens/"+token.id.String(), "")
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke: %d: %s", res.StatusCode, raw)
	}

	select {
	case e := <-ended:
		took := e.at.Sub(revokedAt)
		t.Logf("the stream ended %s after the revocation", took)

		// One poll interval, and a query on top of it: the permission is read
		// again at every poll.
		if took > 1500*time.Millisecond {
			t.Errorf("the stream ended %s after the revocation, want within about a second", took)
		}
		if e.event != "error" {
			t.Errorf("the stream ended with %q, want an error event", e.event)
		}
		// It says that it stopped, and not why: the same words as for a right
		// that was removed, and nothing about tokens.
		if !strings.Contains(e.data, "the log stream stopped") || strings.Contains(strings.ToLower(e.data), "token") {
			t.Errorf("the stream said %q", e.data)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the stream was still open 5 seconds after its token was revoked")
	}
}

// --- A pipeline takes down its own load ----------------------------------------------

// A cancellation by a token, end to end. What it holds in place: a token cancels
// the run it launched and is told so; it cannot touch a run a person launched or
// one another token of the same project launched, and is told those runs are not
// there; and both the cancellation and the refusals are in the journal under the
// token's name.
func TestATokenCancelsTheRunItLaunchedAndNoOtherAndTheJournalSaysSo(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newCIWorld(ctx, t)
	pipeline := w.newToken(t, "ci-main")
	another := w.newToken(t, "ci-other")

	own := w.queueAs(t, pipeline.secret)
	ofAnotherToken := w.queueAs(t, another.secret)

	// A run a person launched, in the same project.
	res, raw := w.call(t, w.asAdmin(), http.MethodPost, "/runs", w.runBody(w.a))
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("queue a run as a person: %d: %s", res.StatusCode, raw)
	}
	var byPerson struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &byPerson); err != nil {
		t.Fatalf("read the run: %v: %s", err, raw)
	}

	statusOf := func(id string) string {
		t.Helper()
		var status string
		if err := w.db.InTenantTx(ctx, w.org, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT status FROM run WHERE org_id = $1 AND id = $2`, w.org, id).Scan(&status)
		}); err != nil {
			t.Fatalf("read the status of %s: %v", id, err)
		}
		return status
	}

	// Refused first, and nothing moves.
	var refusals [][]byte
	for name, id := range map[string]string{"a person's run": byPerson.ID, "another token's run": ofAnotherToken} {
		res, raw := w.call(t, asPipeline(pipeline.secret), http.MethodDelete, "/runs/"+id, "")
		if res.StatusCode != http.StatusNotFound {
			t.Errorf("cancel %s: %d, want 404: %s", name, res.StatusCode, raw)
		}
		if got := statusOf(id); got != "queued" {
			t.Errorf("%s is %q after a refused cancellation, want queued", name, got)
		}
		refusals = append(refusals, raw)
	}
	// Told as a run that does not exist is told, so neither confirms the other.
	_, unknown := w.call(t, asPipeline(pipeline.secret), http.MethodDelete, "/runs/"+uuid.NewString(), "")
	for _, raw := range refusals {
		if string(raw) != string(unknown) {
			t.Errorf("a refusal reads %s, a run that is not there reads %s", raw, unknown)
		}
	}

	// Its own run is cancelled, at once: it was queued and held no target.
	res, raw = w.call(t, asPipeline(pipeline.secret), http.MethodDelete, "/runs/"+own, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("cancel its own run: %d: %s", res.StatusCode, raw)
	}
	if got := statusOf(own); got != "cancelled" {
		t.Errorf("its own run is %q, want cancelled", got)
	}

	// The journal: the cancellation and the two refusals of runs that exist, each
	// under the token and under no person.
	type entry struct {
		outcome string
		person  *uuid.UUID
		token   *uuid.UUID
	}
	var entries []entry
	err := w.db.InTenantTx(ctx, w.org, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT outcome, actor_id, actor_token_id FROM audit_log
			WHERE org_id = $1 AND action = 'run.cancelled' ORDER BY created_at`, w.org)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e entry
			if err := rows.Scan(&e.outcome, &e.person, &e.token); err != nil {
				return err
			}
			entries = append(entries, e)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("read the journal: %v", err)
	}

	var allowed, refused int
	for _, e := range entries {
		if e.token == nil || *e.token != pipeline.id || e.person != nil {
			t.Errorf("an entry names person %v and token %v, want the token alone (%s)", e.person, e.token, pipeline.id)
		}
		switch e.outcome {
		case "allowed":
			allowed++
		case "refused":
			refused++
		}
	}
	// A run that does not exist is not in the journal: there is no project to
	// attach the entry to, and nobody was stopped from doing anything to a run.
	if len(entries) != 3 || allowed != 1 || refused != 2 {
		t.Errorf("%d entries, %d allowed and %d refused, want 3: its own cancellation, and the refusals for a person's run and another token's",
			len(entries), allowed, refused)
	}
}

// A token of another project holds nothing in this one and cancels nothing in it,
// even a run of its own kind. Its refusal is the same as for any other run that
// is not there.
func TestATokenCannotCancelARunInAnotherProject(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newCIWorld(ctx, t)
	token := w.newToken(t, "ci-main")
	inB := w.aRunOfB(ctx, t)

	res, raw := w.call(t, asPipeline(token.secret), http.MethodDelete, "/runs/"+inB, "")
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("cancel a run of another project: %d, want 404: %s", res.StatusCode, raw)
	}

	var status string
	if err := w.db.InTenantTx(ctx, w.org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT status FROM run WHERE org_id = $1 AND id = $2`, w.org, inB).Scan(&status)
	}); err != nil {
		t.Fatalf("read the run: %v", err)
	}
	if status != "queued" {
		t.Errorf("the run of another project is %q, want queued", status)
	}
}

// --- What the polling contract promises ----------------------------------------------

// The documentation tells a pipeline how to wait, and these are its promises
// stated against the real API: a run it cancelled is over and has no verdict, and
// a token that has been revoked is a 401 — final, never a thing to retry.
func TestThePollingContractHoldsForACancelledRunAndARevokedToken(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newCIWorld(ctx, t)
	token := w.newToken(t, "ci-main")
	id := w.queueAs(t, token.secret)

	if res, raw := w.call(t, asPipeline(token.secret), http.MethodDelete, "/runs/"+id, ""); res.StatusCode != http.StatusOK {
		t.Fatalf("cancel its own run: %d: %s", res.StatusCode, raw)
	}

	res, raw := w.call(t, asPipeline(token.secret), http.MethodGet, "/runs/"+id, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("read the cancelled run: %d: %s", res.StatusCode, raw)
	}
	var got struct {
		Status  string `json:"status"`
		Verdict string `json:"verdict"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("read the run: %v: %s", err, raw)
	}
	// Terminal, and nothing was judged: the client stops waiting, and does not
	// read "none" as success.
	if got.Status != "cancelled" || got.Verdict != "none" {
		t.Errorf("status %q and verdict %q, want cancelled and none", got.Status, got.Verdict)
	}

	// Revoked: the same read is now a 401, which the documentation calls final.
	if res, raw := w.call(t, w.asAdmin(), http.MethodDelete,
		"/projects/"+w.a.projectID.String()+"/api-tokens/"+token.id.String(), ""); res.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke: %d: %s", res.StatusCode, raw)
	}
	for _, path := range []string{"/runs/" + id, "/runs/" + id + "/logs"} {
		if res, raw := w.call(t, asPipeline(token.secret), http.MethodGet, path, ""); res.StatusCode != http.StatusUnauthorized {
			t.Errorf("GET %s with a revoked token: %d, want 401: %s", path, res.StatusCode, raw)
		}
	}
}

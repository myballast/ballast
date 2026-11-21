//go:build integration

package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// A pipeline says what it is when it launches a run, and the run's page says it
// back: end to end, on the real stack. What these tests hold in place:
//
//   - what the pipeline said is kept in the form it is kept — the commit in lower
//     case, the link without its query — and read back on the run's page, and only
//     there: not in the list, not in the journal;
//   - a mistake in it is a 422 that names the field and never the value, and writes
//     and journals nothing;
//   - a repeat of the run says the same, for the page of the repeat says where what
//     it repeats came from.

// withCI is the body of a run launch with a ci object, built as JSON so that what
// is hostile in it travels as the pipeline would send it.
func (w ciWorld) withCI(t *testing.T, tn tenant, ci any) string {
	t.Helper()

	body := map[string]any{
		"scenario_id":    tn.scenarioID,
		"environment_id": tn.environmentID,
		"profile":        "smoke",
		"params":         map[string]any{"target_vus": 1, "plateau_s": 1},
	}
	if ci != nil {
		body["ci"] = ci
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("build the body: %v", err)
	}
	return string(raw)
}

type readRun struct {
	ID string                     `json:"id"`
	CI map[string]json.RawMessage `json:"ci"`
}

func (w ciWorld) readRun(t *testing.T, id string) (readRun, string) {
	t.Helper()

	res, raw := w.call(t, w.asAdmin(), http.MethodGet, "/runs/"+id, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("read the run: %d: %s", res.StatusCode, raw)
	}
	var run readRun
	if err := json.Unmarshal(raw, &run); err != nil {
		t.Fatalf("read the run: %v: %s", err, raw)
	}
	return run, string(raw)
}

func ciField(t *testing.T, run readRun, key string) string {
	t.Helper()

	raw, ok := run.CI[key]
	if !ok {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("ci.%s is not a string: %v", key, err)
	}
	return value
}

func (w ciWorld) queue(t *testing.T, who as, body string) string {
	t.Helper()

	res, raw := w.call(t, who, http.MethodPost, "/runs", body)
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("queue a run: %d: %s", res.StatusCode, raw)
	}
	var queued struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &queued); err != nil || queued.ID == "" {
		t.Fatalf("the response is not a queued run: %v: %s", err, raw)
	}
	return queued.ID
}

// --- What the pipeline said --------------------------------------------------------------

func TestAPipelineSaysWhatItIsAndTheRunsPageSaysItBack(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newCIWorld(ctx, t)
	token := w.newToken(t, "ci-main")

	id := w.queue(t, asPipeline(token.secret), w.withCI(t, w.a, map[string]any{
		"provider":   "github_actions",
		"repository": "myballast/ballast",
		"branch":     "feature/ci-metadata",
		// Upper case, as some tools write it, and a link whose query carries a token.
		"commit":  "3F9C2A1B7D4E5F60",
		"job_url": "https://github.com/myballast/ballast/actions/runs/42?token=SECRETQUERY#L10",
	}))

	run, raw := w.readRun(t, id)

	want := map[string]string{
		"provider":   "github_actions",
		"repository": "myballast/ballast",
		"branch":     "feature/ci-metadata",
		"commit":     "3f9c2a1b7d4e5f60",
		"job_url":    "https://github.com/myballast/ballast/actions/runs/42",
	}
	for key, value := range want {
		if got := ciField(t, run, key); got != value {
			t.Errorf("ci.%s = %q, want %q", key, got, value)
		}
	}
	if strings.Contains(raw, "SECRETQUERY") {
		t.Errorf("the page of the run carries what was in the link's query: %s", raw)
	}

	// In the row, each in its own column.
	var provider, commit, jobURL *string
	err := w.db.InTenantTx(ctx, w.org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT ci_provider, ci_commit, ci_job_url FROM run WHERE id = $1`, id).
			Scan(&provider, &commit, &jobURL)
	})
	if err != nil {
		t.Fatalf("read the columns: %v", err)
	}
	if provider == nil || *provider != "github_actions" || commit == nil || *commit != "3f9c2a1b7d4e5f60" || jobURL == nil || strings.Contains(*jobURL, "?") {
		t.Errorf("the columns hold %v / %v / %v", provider, commit, jobURL)
	}
}

// What a pipeline said is not for the journal: the entry about the run keeps the
// four details it always had, and none of the words.
func TestWhatThePipelineSaidIsNotInTheJournal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newCIWorld(ctx, t)
	token := w.newToken(t, "ci-main")
	w.queue(t, asPipeline(token.secret), w.withCI(t, w.a, map[string]any{
		"provider": "gitlab_ci", "repository": "group/project", "branch": "main", "commit": "abcdef1234",
		"job_url": "https://gitlab.com/group/project/-/jobs/42",
	}))

	var details string
	err := w.db.InTenantTx(ctx, w.org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT metadata::text FROM audit_log WHERE org_id = $1 AND action = 'run.queued'`, w.org).Scan(&details)
	})
	if err != nil {
		t.Fatalf("read the entry: %v", err)
	}
	for _, said := range []string{"gitlab", "group/project", "abcdef1234", "jobs/42"} {
		if strings.Contains(strings.ToLower(details), said) {
			t.Errorf("the entry carries %q: %s", said, details)
		}
	}
}

// A person launches a run with it too: it is declared by whoever calls, and the
// page says so the same way.
func TestAPersonCanSayItToo(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newCIWorld(ctx, t)

	id := w.queue(t, w.asAdmin(), w.withCI(t, w.a, map[string]any{"provider": "other", "branch": "main"}))

	run, _ := w.readRun(t, id)
	if ciField(t, run, "provider") != "other" || ciField(t, run, "branch") != "main" {
		t.Errorf("the page says %v", run.CI)
	}
	// And a field nobody gave is not in the response, as empty or as anything.
	for _, key := range []string{"repository", "commit", "job_url"} {
		if _, there := run.CI[key]; there {
			t.Errorf("ci.%s is in the response for a field nobody gave", key)
		}
	}
}

func TestARunNobodySaidAnythingAboutHasNoCIKey(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newCIWorld(ctx, t)

	id := w.queue(t, w.asAdmin(), w.withCI(t, w.a, nil))

	if _, raw := w.readRun(t, id); strings.Contains(raw, `"ci"`) {
		t.Errorf("a run with no CI data has a ci key: %s", raw)
	}
}

// The list is read for the runs of a whole project, and the context of each is read
// on its own page.
func TestTheListOfRunsDoesNotCarryIt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newCIWorld(ctx, t)
	w.queue(t, w.asAdmin(), w.withCI(t, w.a, map[string]any{"provider": "github_actions", "branch": "main"}))

	res, raw := w.call(t, w.asAdmin(), http.MethodGet, "/projects/"+w.a.projectID.String()+"/runs", "")
	if res.StatusCode != http.StatusOK || strings.Contains(string(raw), `"ci"`) || strings.Contains(string(raw), "github_actions") {
		t.Errorf("the list: %d: %s", res.StatusCode, raw)
	}
}

// --- A mistake in it ---------------------------------------------------------------------

func TestAMistakeInTheCIDataIsA422ThatNamesTheFieldAndNeverTheValue(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newCIWorld(ctx, t)
	token := w.newToken(t, "ci-main")

	for name, c := range map[string]struct {
		ci     map[string]any
		field  string
		secret string
	}{
		"a credential in the link": {
			map[string]any{"provider": "other", "job_url": "https://user:hunter2@ci.example.test/job"}, "job_url", "hunter2",
		},
		"a plain link": {
			map[string]any{"provider": "other", "job_url": "http://ci.example.test/job-SENTINEL"}, "job_url", "SENTINEL",
		},
		"a branch with a line break": {
			map[string]any{"provider": "other", "branch": "main\nBallast: passed-SENTINEL"}, "branch", "SENTINEL",
		},
		// A right-to-left override makes "release" read backwards, and whatever
		// follows read the other way round.
		"a branch that reads backwards": {
			map[string]any{"provider": "other", "branch": "release\u202egnp.exe-SENTINEL"}, "branch", "SENTINEL",
		},
		"a commit that is not a hash": {
			map[string]any{"provider": "other", "commit": "main-SENTINEL"}, "commit", "SENTINEL",
		},
		"a provider nobody knows": {
			map[string]any{"provider": "jenkins-SENTINEL"}, "provider", "SENTINEL",
		},
		"no provider": {
			map[string]any{"repository": "owner/repo"}, "provider", "",
		},
		"an empty object": {
			map[string]any{}, "provider", "",
		},
		"a repository that goes up": {
			map[string]any{"provider": "other", "repository": "../etc/passwd-SENTINEL"}, "repository", "SENTINEL",
		},
	} {
		res, raw := w.call(t, asPipeline(token.secret), http.MethodPost, "/runs", w.withCI(t, w.a, c.ci))

		if res.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("%s: %d, want 422: %s", name, res.StatusCode, raw)
			continue
		}
		if !strings.Contains(string(raw), c.field) {
			t.Errorf("%s: the response does not name the field %q: %s", name, c.field, raw)
		}
		if c.secret != "" && strings.Contains(string(raw), c.secret) {
			t.Errorf("%s: the response repeats what was sent: %s", name, raw)
		}
	}

	// Nothing written, and nothing journaled: a typing mistake is not somebody
	// being stopped from doing something.
	if n := w.count(ctx, t, `SELECT count(*) FROM run WHERE org_id = $1`, w.org); n != 0 {
		t.Errorf("%d runs were written for requests that were refused", n)
	}
	if n := w.count(ctx, t, `SELECT count(*) FROM audit_log WHERE org_id = $1 AND action LIKE 'run.%'`, w.org); n != 0 {
		t.Errorf("%d journal entries were written for mistakes in the request", n)
	}
}

// --- A repeat -----------------------------------------------------------------------------

func TestARepeatOfARunSaysWhereWhatItRepeatsCameFrom(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newCIWorld(ctx, t)
	token := w.newToken(t, "ci-main")
	first := w.queue(t, asPipeline(token.secret), w.withCI(t, w.a, map[string]any{
		"provider": "github_actions", "repository": "myballast/ballast", "branch": "main", "commit": "abcdef1",
		"job_url": "https://github.com/myballast/ballast/actions/runs/42",
	}))

	// An administrator repeats it from the page.
	res, raw := w.call(t, w.asAdmin(), http.MethodPost, "/runs/"+first+"/rerun", "")
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("repeat the run: %d: %s", res.StatusCode, raw)
	}
	var repeat struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &repeat); err != nil || repeat.ID == "" || repeat.ID == first {
		t.Fatalf("the response is not a new run: %v: %s", err, raw)
	}

	original, _ := w.readRun(t, first)
	repeated, _ := w.readRun(t, repeat.ID)
	for _, key := range []string{"provider", "repository", "branch", "commit", "job_url"} {
		if ciField(t, repeated, key) != ciField(t, original, key) || ciField(t, repeated, key) == "" {
			t.Errorf("ci.%s: the repeat says %q, the run it repeats says %q", key, ciField(t, repeated, key), ciField(t, original, key))
		}
	}
}

func TestARepeatOfARunWithNoCIDataHasNone(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newCIWorld(ctx, t)
	first := w.queue(t, w.asAdmin(), w.withCI(t, w.a, nil))

	res, raw := w.call(t, w.asAdmin(), http.MethodPost, "/runs/"+first+"/rerun", "")
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("repeat the run: %d: %s", res.StatusCode, raw)
	}
	var repeat struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &repeat)

	if _, page := w.readRun(t, repeat.ID); strings.Contains(page, `"ci"`) {
		t.Errorf("the repeat of a run with no CI data has some: %s", page)
	}
	if _, err := uuid.Parse(repeat.ID); err != nil {
		t.Errorf("the repeat has no id: %q", repeat.ID)
	}
}

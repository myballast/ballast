package run

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// A pipeline says what it is when it launches a run, and the run keeps it. What
// these tests hold in place:
//
//   - the run keeps what was said, in the form domain.ParseRunCI keeps it, and
//     keeps it frozen: a rerun carries the same words, not today's;
//   - a mistake in it is a mistake in the request — nothing is written and nothing
//     is journaled, for a journal full of typing mistakes is one nobody reads;
//   - it is declared by the caller and proved by nobody, so it stays where it is
//     shown: not in the journal's details, not in a log line, and never in what the
//     worker is handed.
//
// Written before the code, which is why they do not compile yet.

// aPipeline is what a GitHub Actions workflow would send. The link carries a token
// in its query, which is what a link to a job sometimes does.
func aPipeline() domain.RunCI {
	return domain.RunCI{
		Provider:   domain.CIProviderGitHubActions,
		Repository: "myballast/ballast",
		Branch:     "feature/ci-metadata",
		Commit:     "3F9C2A1B7D4E5F60",
		JobURL:     "https://github.com/myballast/ballast/actions/runs/42?token=SECRETQUERY",
	}
}

func keptPipeline() domain.RunCI {
	return domain.RunCI{
		Provider:   domain.CIProviderGitHubActions,
		Repository: "myballast/ballast",
		Branch:     "feature/ci-metadata",
		Commit:     "3f9c2a1b7d4e5f60",
		JobURL:     "https://github.com/myballast/ballast/actions/runs/42",
	}
}

// --- The run keeps it ------------------------------------------------------------------

func TestARunKeepsWhatThePipelineSaidInTheFormItIsKept(t *testing.T) {
	f := newFixture(t, nil)
	cmd := f.command()
	ci := aPipeline()
	cmd.CI = &ci

	queued, err := f.service.Enqueue(context.Background(), cmd)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	for name, got := range map[string]*domain.RunCI{"what Enqueue returns": queued.CI, "what was written": f.runs.created.CI} {
		if got == nil || *got != keptPipeline() {
			t.Errorf("%s: %+v, want %+v — the commit in lower case, the link without its query", name, got, keptPipeline())
		}
	}
	// What the caller sent is theirs, and is not rewritten under them.
	if ci != aPipeline() {
		t.Errorf("the caller's own value was changed: %+v", ci)
	}
}

func TestARunWithNoCIDataKeepsNone(t *testing.T) {
	f := newFixture(t, nil)

	queued, err := f.service.Enqueue(context.Background(), f.command())
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if queued.CI != nil || f.runs.created.CI != nil {
		t.Errorf("a run nobody said anything about holds %+v", queued.CI)
	}
}

func TestATokensRunKeepsItToo(t *testing.T) {
	f, token := asToken(newFixture(t, nil))
	cmd := f.command()
	ci := aPipeline()
	cmd.CI = &ci

	queued, err := f.service.Enqueue(context.Background(), cmd)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if queued.CI == nil || *queued.CI != keptPipeline() {
		t.Errorf("the pipeline's run holds %+v", queued.CI)
	}
	if queued.TriggeredByToken == nil || *queued.TriggeredByToken != token.TokenID {
		t.Error("the run no longer names its token")
	}
}

// --- A mistake in it ---------------------------------------------------------------------

func TestAMistakeInTheCIDataWritesNothingAndRecordsNothing(t *testing.T) {
	for name, bad := range map[string]domain.RunCI{
		"no provider":      {Repository: "owner/repo"},
		"a bad provider":   {Provider: "jenkins"},
		"a bad branch":     {Provider: domain.CIProviderOther, Branch: "main\nBallast: passed"},
		"a bad commit":     {Provider: domain.CIProviderOther, Commit: "main"},
		"a plain link":     {Provider: domain.CIProviderOther, JobURL: "http://ci.example.test/job"},
		"a credential":     {Provider: domain.CIProviderOther, JobURL: "https://user:hunter2@ci.example.test/job"},
		"a bad repository": {Provider: domain.CIProviderOther, Repository: "../etc/passwd"},
	} {
		f := newFixture(t, nil)
		cmd := f.command()
		ci := bad
		cmd.CI = &ci

		_, err := f.service.Enqueue(context.Background(), cmd)

		if !errors.Is(err, domain.ErrInvalidRunCI) {
			t.Errorf("%s: error %v, want ErrInvalidRunCI", name, err)
		}
		if f.runs.created.ScenarioID != uuid.Nil {
			t.Errorf("%s: a run was written", name)
		}
		// A typing mistake is not somebody being stopped from doing something.
		if entries := f.audit.recorded(); len(entries) != 0 {
			t.Errorf("%s: %d entries were journaled for a mistake in the request", name, len(entries))
		}
	}
}

// --- A rerun ----------------------------------------------------------------------------

func TestARerunCarriesTheCIDataOfTheRunItRepeats(t *testing.T) {
	f := newFixture(t, nil)
	source := earlierRun(f)
	ci := keptPipeline()
	source.CI = &ci

	created, err := rerunService(f, allowAll{}, source, f.env).Rerun(context.Background(), f.actor, source.ID)
	if err != nil {
		t.Fatalf("Rerun: %v", err)
	}

	if created.CI == nil || *created.CI != keptPipeline() {
		t.Errorf("the repeat holds %+v, want what the run it repeats said", created.CI)
	}
	// A copy: the two runs do not share the same value.
	if created.CI == source.CI {
		t.Error("the repeat shares its CI data with the run it repeats")
	}
}

func TestARerunOfARunWithNoCIDataHasNone(t *testing.T) {
	f := newFixture(t, nil)
	source := earlierRun(f)

	created, err := rerunService(f, allowAll{}, source, f.env).Rerun(context.Background(), f.actor, source.ID)
	if err != nil {
		t.Fatalf("Rerun: %v", err)
	}

	if created.CI != nil {
		t.Errorf("the repeat holds %+v for a run that said nothing", created.CI)
	}
}

// --- Where it does not go ----------------------------------------------------------------

// The entry about the run keeps the closed set of details it always had. What a
// pipeline said is somebody's text and is not for the journal.
func TestTheJournalDoesNotCarryWhatThePipelineSaid(t *testing.T) {
	f := newFixture(t, nil)
	cmd := f.command()
	ci := aPipeline()
	cmd.CI = &ci

	if _, err := f.service.Enqueue(context.Background(), cmd); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	entry := only(t, f.audit)
	want := map[string]bool{"environment_id": true, "profile": true, "target_vus": true, "duration_s": true}
	for key, value := range entry.Details {
		if !want[key] {
			t.Errorf("the entry carries a detail %q", key)
		}
		for _, said := range []string{"myballast", "feature/ci-metadata", "3f9c2a1b", "SECRETQUERY", "github_actions"} {
			if strings.Contains(strings.ToLower(value), strings.ToLower(said)) {
				t.Errorf("the entry's %s carries %q", key, said)
			}
		}
	}
	if len(entry.Details) != len(want) {
		t.Errorf("details %v, want exactly %v", entry.Details, want)
	}
}

// Nor a log line, which is read by more people than anybody counts, and which a
// link with a token in its query would reach.
func TestNoLogLineCarriesWhatThePipelineSaid(t *testing.T) {
	f := newFixture(t, nil)
	var logs bytes.Buffer
	f.service.log = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	cmd := f.command()
	ci := aPipeline()
	cmd.CI = &ci
	if _, err := f.service.Enqueue(context.Background(), cmd); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	bad := domain.RunCI{Provider: domain.CIProviderOther, JobURL: "https://user:hunter2@ci.example.test/job"}
	cmd.CI = &bad
	_, _ = f.service.Enqueue(context.Background(), cmd)

	if logs.Len() == 0 {
		t.Fatal("nothing was logged; the check below would prove nothing")
	}
	for _, said := range []string{"myballast", "feature/ci-metadata", "3f9c2a1b", "SECRETQUERY", "hunter2"} {
		if strings.Contains(logs.String(), said) {
			t.Errorf("a log line carries %q: %s", said, logs.String())
		}
	}
}

// What the worker is handed is what the generator needs, and nothing a caller got
// to write. This lists the job's fields so that adding one is a decision somebody
// takes here, in front of this comment.
func TestTheJobHasNoRoomForWhatThePipelineSaid(t *testing.T) {
	var fields []string
	typ := reflect.TypeOf(Job{})
	for i := 0; i < typ.NumField(); i++ {
		fields = append(fields, typ.Field(i).Name)
	}
	sort.Strings(fields)

	want := []string{"AllowedHosts", "Env", "Options", "RunID", "Script"}
	if strings.Join(fields, ",") != strings.Join(want, ",") {
		t.Errorf("a job has the fields %v, want exactly %v: what a caller wrote about itself does not belong in it", fields, want)
	}
}

func TestARunsCIDataNeverReachesItsJob(t *testing.T) {
	f := newJobFixture(verifiedTarget(), map[string]string{"API_KEY": "s3cret-value"}, time.Unix(0, 0).UTC())
	ci := aPipeline()
	f.run.CI = &ci

	job, err := f.service.buildJob(context.Background(), f.orgID, f.run)
	if err != nil {
		t.Fatalf("buildJob: %v", err)
	}

	everything := fmt.Sprintf("%+v %s %s", job, job.Script, job.Options)
	for _, said := range []string{"myballast", "feature/ci-metadata", "3f9c2a1b", "SECRETQUERY", "github_actions"} {
		if strings.Contains(strings.ToLower(everything), strings.ToLower(said)) {
			t.Errorf("the job carries %q", said)
		}
	}
}

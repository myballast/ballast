//go:build integration

package e2e

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The command line tool, as a pipeline runs it: the real binary, built from the real
// source, against the real API and the real database, with a real token.
//
// What these tests hold in place, end to end:
//
//   - a run the tool launched is a run of that token, carrying what the pipeline said
//     about itself, and a run the tool gives up on is cancelled and written to the
//     journal under the token's name;
//   - a signal is answered by asking for the run to stop before the tool leaves, and
//     the code is 2;
//   - the exit code is 0 when the thresholds held and 1 when one was breached, read
//     from a run that ended;
//   - a refusal is printed as the server said it, and nothing was queued;
//   - the token is in none of what the tool printed.
//
// No container is started: nothing here is ever executed, and the runs that end do so
// because the test wrote their ending, the way a worker would.

var (
	cliBuild  sync.Once
	cliPath   string
	cliBuilt  error
	cliOutput []byte
)

// theTool builds the binary once for the whole package.
func theTool(t *testing.T) string {
	t.Helper()

	cliBuild.Do(func() {
		dir, err := os.MkdirTemp("", "ballast-cli-")
		if err != nil {
			cliBuilt = err
			return
		}
		cliPath = dir + "/ballast"
		cliOutput, cliBuilt = exec.Command("go", "build", "-o", cliPath, "../../cmd/ballast").CombinedOutput()
	})
	if cliBuilt != nil {
		t.Fatalf("build the tool: %v\n%s", cliBuilt, cliOutput)
	}
	return cliPath
}

type toolRun struct {
	cmd      *exec.Cmd
	out, err *bytes.Buffer
	done     chan struct{}
}

// startTool starts the tool the way a pipeline does: a bare environment holding what
// the tool reads, and nothing else of this machine's.
func (w ciWorld) startTool(t *testing.T, secret string, environment []string, args ...string) *toolRun {
	t.Helper()

	run := &toolRun{out: &bytes.Buffer{}, err: &bytes.Buffer{}, done: make(chan struct{})}
	run.cmd = exec.Command(theTool(t), args...)
	run.cmd.Env = append([]string{"BALLAST_URL=" + w.api.URL, "BALLAST_TOKEN=" + secret}, environment...)
	run.cmd.Stdout, run.cmd.Stderr = run.out, run.err

	if err := run.cmd.Start(); err != nil {
		t.Fatalf("start the tool: %v", err)
	}
	t.Cleanup(func() {
		if run.cmd.ProcessState == nil {
			_ = run.cmd.Process.Kill()
		}
	})
	go func() {
		_ = run.cmd.Wait()
		close(run.done)
	}()
	return run
}

// finish waits for the tool and returns the code it left with.
func (r *toolRun) finish(t *testing.T) int {
	t.Helper()

	select {
	case <-r.done:
	case <-time.After(40 * time.Second):
		_ = r.cmd.Process.Kill()
		t.Fatalf("the tool did not leave in 40 seconds\nstdout: %s\nstderr: %s", r.out, r.err)
	}
	return r.cmd.ProcessState.ExitCode()
}

func (r *toolRun) all() string { return r.out.String() + "\n" + r.err.String() }

func (w ciWorld) toolArgs(extra ...string) []string {
	return append([]string{
		"run", "--scenario", w.a.scenarioID.String(), "--environment", w.a.environmentID.String(), "--profile", "smoke",
	}, extra...)
}

// theRun waits for the run the tool launched and returns it.
func (w ciWorld) theRun(ctx context.Context, t *testing.T) uuid.UUID {
	t.Helper()

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var id uuid.UUID
		err := w.db.InTenantTx(ctx, w.org, func(tx pgx.Tx) error {
			err := tx.QueryRow(ctx, `SELECT id FROM run WHERE org_id = $1 LIMIT 1`, w.org).Scan(&id)
			if err == pgx.ErrNoRows {
				return nil
			}
			return err
		})
		if err != nil {
			t.Fatalf("look for the run: %v", err)
		}
		if id != uuid.Nil {
			return id
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("the tool launched no run in 15 seconds")
	return uuid.Nil
}

// ended writes the end of a run the way a worker does: its status, and the verdicts k6
// gave on its thresholds.
func (w ciWorld) ended(ctx context.Context, t *testing.T, id uuid.UUID, status string, thresholds map[string]bool) {
	t.Helper()

	err := w.db.InTenantTx(ctx, w.org, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE run SET status = $3, started_at = now(), ended_at = now()
			WHERE org_id = $1 AND id = $2`, w.org, id, status); err != nil {
			return err
		}
		for expression, passed := range thresholds {
			if _, err := tx.Exec(ctx, `INSERT INTO run_threshold (org_id, run_id, metric, expression, passed)
				VALUES ($1, $2, 'http_req_duration', $3, $4)`, w.org, id, expression, passed); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("end the run: %v", err)
	}
}

func (w ciWorld) statusOf(ctx context.Context, t *testing.T, id uuid.UUID) (status string, byToken *uuid.UUID, provider, commit *string) {
	t.Helper()

	err := w.db.InTenantTx(ctx, w.org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT status, triggered_by_token, ci_provider, ci_commit
			FROM run WHERE org_id = $1 AND id = $2`, w.org, id).Scan(&status, &byToken, &provider, &commit)
	})
	if err != nil {
		t.Fatalf("read the run: %v", err)
	}
	return status, byToken, provider, commit
}

func neverTheToken(t *testing.T, secret string, r *toolRun) {
	t.Helper()

	if shown := r.all(); strings.Contains(shown, secret) || strings.Contains(shown, secret[4:]) {
		t.Errorf("the token was printed:\n%s", shown)
	}
}

// --- Giving up on a run -----------------------------------------------------------------------------

func TestTheToolCancelsItsRunWhenItsTimeIsUpAndTheJournalSaysSo(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newCIWorld(ctx, t)
	token := w.newToken(t, "ci-main")

	tool := w.startTool(t, token.secret, []string{
		"GITHUB_ACTIONS=true", "GITHUB_REPOSITORY=myballast/ballast", "GITHUB_REF_NAME=main", "GITHUB_REF_TYPE=branch",
		"GITHUB_SHA=3F9C2A1B7D4E5F60718293A4B5C6D7E8F9012345",
		"GITHUB_SERVER_URL=https://github.com", "GITHUB_RUN_ID=42",
	}, w.toolArgs("--timeout", "2s")...)

	if code := tool.finish(t); code != 2 {
		t.Fatalf("exit %d, want 2\n%s", code, tool.all())
	}

	id := w.theRun(ctx, t)
	status, byToken, provider, commit := w.statusOf(ctx, t, id)
	if status != "cancelled" {
		t.Errorf("the run is %q, want cancelled: the tool left a run behind", status)
	}
	if byToken == nil || *byToken != token.id {
		t.Errorf("the run belongs to token %v, want %s", byToken, token.id)
	}
	// What the pipeline said about itself, as the server keeps it.
	if provider == nil || *provider != "github_actions" || commit == nil || *commit != "3f9c2a1b7d4e5f60718293a4b5c6d7e8f9012345" {
		t.Errorf("ci data %v / %v", provider, commit)
	}

	var actor *uuid.UUID
	var outcome string
	err := w.db.InTenantTx(ctx, w.org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT outcome, actor_token_id FROM audit_log
			WHERE org_id = $1 AND action = 'run.cancelled'`, w.org).Scan(&outcome, &actor)
	})
	if err != nil {
		t.Fatalf("read the journal: %v", err)
	}
	if outcome != "allowed" || actor == nil || *actor != token.id {
		t.Errorf("the journal says %s by %v, want the token's cancellation", outcome, actor)
	}

	if !strings.Contains(tool.err.String(), "timeout") || !strings.Contains(tool.err.String(), id.String()) {
		t.Errorf("the tool does not say why it stopped or which run:\n%s", tool.err)
	}
	neverTheToken(t, token.secret, tool)
}

// A real signal, delivered to a real process, answered by a real request.
func TestASignalAsksTheRunToStopBeforeTheToolLeaves(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newCIWorld(ctx, t)
	token := w.newToken(t, "ci-main")

	tool := w.startTool(t, token.secret, nil, w.toolArgs()...)
	id := w.theRun(ctx, t)
	// Let it read the run once, so that it is waiting and not still launching.
	time.Sleep(300 * time.Millisecond)

	if err := tool.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signal: %v", err)
	}

	if code := tool.finish(t); code != 2 {
		t.Fatalf("exit %d, want 2\n%s", code, tool.all())
	}
	if status, _, _, _ := w.statusOf(ctx, t, id); status != "cancelled" {
		t.Errorf("the run is %q, want cancelled", status)
	}
	if !strings.Contains(tool.err.String(), "interrupted") {
		t.Errorf("the tool does not say it was interrupted:\n%s", tool.err)
	}
	neverTheToken(t, token.secret, tool)
}

// --- Leaving with what the run was -------------------------------------------------------------------

func TestTheToolLeavesWithZeroWhenTheThresholdsHeld(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newCIWorld(ctx, t)
	token := w.newToken(t, "ci-main")

	tool := w.startTool(t, token.secret, nil, w.toolArgs()...)
	id := w.theRun(ctx, t)
	w.ended(ctx, t, id, "passed", map[string]bool{"p(95)<500": true})

	if code := tool.finish(t); code != 0 {
		t.Fatalf("exit %d, want 0\n%s", code, tool.all())
	}
	for _, want := range []string{"p(95)<500", "Verdict: met", "http://web.test/runs/" + id.String()} {
		if !strings.Contains(tool.out.String(), want) {
			t.Errorf("stdout lacks %q:\n%s", want, tool.out)
		}
	}
	neverTheToken(t, token.secret, tool)
}

func TestTheToolLeavesWithOneWhenAThresholdWasBreached(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newCIWorld(ctx, t)
	token := w.newToken(t, "ci-main")

	tool := w.startTool(t, token.secret, nil, w.toolArgs()...)
	id := w.theRun(ctx, t)
	w.ended(ctx, t, id, "failed", map[string]bool{"p(95)<500": false, "p(99)<900": true})

	if code := tool.finish(t); code != 1 {
		t.Fatalf("exit %d, want 1\n%s", code, tool.all())
	}
	if !strings.Contains(tool.out.String(), "BREACHED") || !strings.Contains(tool.out.String(), "p(95)<500") {
		t.Errorf("the table does not show what was breached:\n%s", tool.out)
	}
	neverTheToken(t, token.secret, tool)
}

// --- A refusal -----------------------------------------------------------------------------------------

func TestARefusalIsPrintedAsTheServerSaidItAndNothingIsQueued(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newCIWorld(ctx, t)
	token := w.newToken(t, "ci-main")

	tool := w.startTool(t, token.secret, nil, "run",
		"--scenario", w.a.scenarioID.String(), "--environment", w.aUnverified.String(), "--profile", "smoke")

	if code := tool.finish(t); code != 2 {
		t.Fatalf("exit %d, want 2\n%s", code, tool.all())
	}
	if !strings.Contains(tool.err.String(), "this environment has not been verified") {
		t.Errorf("the server's message is not printed:\n%s", tool.err)
	}
	if n := w.count(ctx, t, `SELECT count(*) FROM run WHERE org_id = $1`, w.org); n != 0 {
		t.Errorf("%d run(s) were queued against an environment nobody proved they own", n)
	}
	if tool.out.Len() != 0 {
		t.Errorf("a refusal printed on stdout: %q", tool.out)
	}
	neverTheToken(t, token.secret, tool)
}

// A token typed as an argument is refused and never launches anything: the only place
// it is read from is the environment.
func TestATokenTypedAsAnArgumentIsRefusedAndNothingIsLaunched(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	w := newCIWorld(ctx, t)
	token := w.newToken(t, "ci-main")

	tool := w.startTool(t, token.secret, nil, w.toolArgs("--token", token.secret)...)

	if code := tool.finish(t); code != 2 {
		t.Fatalf("exit %d, want 2\n%s", code, tool.all())
	}
	if n := w.count(ctx, t, `SELECT count(*) FROM run WHERE org_id = $1`, w.org); n != 0 {
		t.Errorf("%d run(s) were queued", n)
	}
	neverTheToken(t, token.secret, tool)
}

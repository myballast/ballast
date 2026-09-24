package hosted

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// ErrInvalidJob reports a job that cannot be turned into a container run.
var ErrInvalidJob = errors.New("hosted: invalid job")

const (
	// DefaultImage is pinned deliberately. The generator is part of what a run
	// means; letting it float would change results between two runs of the same
	// scenario without anything in the history saying so.
	DefaultImage = "grafana/k6:1.8.1"

	// RunIDLabel is carried by every container Ballast starts. It is how an
	// orphan is found when the process that started it is gone.
	RunIDLabel = "ballast.run_id"

	scriptDir      = "/ballast/script"
	outDir         = "/ballast/out"
	summaryPath    = outDir + "/summary.json"
	userScriptName = "user.js"
	entryName      = "entry.js"

	// The image's own user. A k6 script is arbitrary code someone uploaded, so
	// it never runs as root.
	containerUser = "12345:12345"
)

// Config holds what every run of this runner shares.
type Config struct {
	// Image defaults to DefaultImage.
	Image string
	// CPUs, as a fraction of a core. Defaults to 1.
	CPUs float64
	// MemoryMB defaults to 512.
	MemoryMB int64
	// PidsLimit defaults to 256. A script that forks in a loop is still a
	// script somebody uploaded.
	PidsLimit int64
	// TTL is the hard ceiling on a container's life. Defaults to 15 minutes.
	TTL time.Duration
	// ExtraHosts is passed straight to Docker, in the host:ip form. It exists
	// so a development or test run can reach a server on the host machine. The
	// network is otherwise Docker's default bridge: restricting it to the
	// project's allowed hosts is a separate piece of work, and until it lands
	// this runner does not constrain where a script connects.
	ExtraHosts []string
	// WorkDir is where per-run directories are created. Defaults to the
	// operating system's temporary directory.
	WorkDir string
}

func (c Config) withDefaults() Config {
	if c.Image == "" {
		c.Image = DefaultImage
	}
	if c.CPUs <= 0 {
		c.CPUs = 1
	}
	if c.MemoryMB <= 0 {
		c.MemoryMB = 512
	}
	if c.PidsLimit <= 0 {
		c.PidsLimit = 256
	}
	if c.TTL <= 0 {
		c.TTL = 15 * time.Minute
	}
	return c
}

// Job is one run to execute.
type Job struct {
	RunID uuid.UUID
	// Script is the user's k6 scenario, untouched.
	Script []byte
	// Options is the k6 options block, as produced from the load profile.
	Options []byte
	// Env is injected into the container. It carries the environment's secrets,
	// so nothing here is ever logged.
	Env map[string]string
}

func (j Job) validate() error {
	switch {
	case j.RunID == uuid.Nil:
		return fmt.Errorf("%w: no run id", ErrInvalidJob)
	case len(j.Script) == 0:
		return fmt.Errorf("%w: the scenario is empty", ErrInvalidJob)
	case len(j.Options) == 0:
		return fmt.Errorf("%w: no k6 options", ErrInvalidJob)
	}
	return nil
}

// Result is what a finished container left behind.
type Result struct {
	// Summary is the raw k6 summary. It still contains setup_data at this
	// point; extracting only the allowed fields is the summary parser's job.
	Summary []byte
	// ExitCode is k6's, or the shell's when the time limit intervened.
	ExitCode int64
	// TimedOut reports that the hard TTL killed the container.
	TimedOut bool
}

// Runner executes scenarios in throwaway k6 containers on the Docker daemon
// this process can reach.
type Runner struct {
	cli client.APIClient
	cfg Config

	// Guards the one-off check that the generator image is on the machine.
	imageOnce    imageGuard
	imagePresent bool
}

// New connects to the Docker daemon described by the environment.
func New(cfg Config) (*Runner, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("connect to docker: %w", err)
	}

	return &Runner{cli: cli, cfg: cfg.withDefaults()}, nil
}

// Close releases the Docker connection.
func (r *Runner) Close() error {
	return r.cli.Close()
}

// Run executes job and returns once the container has finished and been
// removed.
//
// Every line the generator writes is sent to logs before Run returns, so the
// caller has to be draining it; a caller with no use for the output passes nil.
// Run never closes the channel, having not created it.
//
// The container is destroyed whatever happens — success, failure, a cancelled
// context, a panic. Two independent mechanisms see to it: the deferred removal
// here, and a time limit running inside the container itself, which is the only
// one that still applies if this process is killed outright.
func (r *Runner) Run(ctx context.Context, job Job, logs chan<- domain.LogLine) (Result, error) {
	if err := job.validate(); err != nil {
		return Result{}, err
	}

	// Before anything else: Docker will not fetch a missing image by itself,
	// and a machine that has never run a test has none of them.
	if err := r.ensureImage(ctx); err != nil {
		return Result{}, err
	}

	dir, err := r.writeScripts(job)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	id, err := r.create(ctx, job, dir)
	if err != nil {
		return Result{}, err
	}
	// Before anything can fail. A cleanup registered later is a cleanup that
	// does not run on the paths that need it most.
	defer r.remove(ctx, id)

	if err := r.cli.ContainerStart(ctx, id, container.StartOptions{}); err != nil {
		return Result{}, fmt.Errorf("start the container: %w", err)
	}

	streamed := r.stream(ctx, id, logs)

	exitCode, err := r.wait(ctx, id)
	// The log stream ends when the container does; wait for it so that no line
	// is lost between the last write and the container's removal.
	<-streamed
	if err != nil {
		return Result{}, err
	}

	// The path is built from the directory this runner created itself, not
	// from anything the job carried.
	summary, err := os.ReadFile(filepath.Join(dir, "out", "summary.json")) //nolint:gosec // path is ours
	if err != nil && !os.IsNotExist(err) {
		return Result{}, fmt.Errorf("read the summary: %w", err)
	}

	return Result{
		Summary:  summary,
		ExitCode: exitCode,
		// The shell reports a process killed by a signal as 128 plus the
		// signal number; SIGKILL is 9.
		TimedOut: exitCode == 137,
	}, nil
}

// writeScripts lays out the per-run directory: the user's scenario and the
// generated entry module in one folder, and an empty folder for the summary.
func (r *Runner) writeScripts(job Job) (string, error) {
	entry, err := buildEntryScript(job.Options)
	if err != nil {
		return "", err
	}

	dir, err := os.MkdirTemp(r.cfg.WorkDir, "ballast-run-")
	if err != nil {
		return "", fmt.Errorf("create the run directory: %w", err)
	}

	scripts := filepath.Join(dir, "script")
	out := filepath.Join(dir, "out")

	// The container runs as an unprivileged user of its own, which is not the
	// user this process runs as, so the folder it writes into has to be open.
	// It holds one generated summary and is deleted when the run ends.
	for path, perm := range map[string]os.FileMode{scripts: 0o755, out: 0o777} {
		if err := os.Mkdir(path, perm); err != nil {
			_ = os.RemoveAll(dir)
			return "", fmt.Errorf("create %s: %w", filepath.Base(path), err)
		}
		if err := os.Chmod(path, perm); err != nil {
			_ = os.RemoveAll(dir)
			return "", fmt.Errorf("set the permissions of %s: %w", filepath.Base(path), err)
		}
	}

	files := map[string][]byte{
		filepath.Join(scripts, userScriptName): job.Script,
		filepath.Join(scripts, entryName):      []byte(entry),
	}
	for path, content := range files {
		// World readable on purpose: the container runs as a user of its own
		// and has to be able to read the scenario it was given. The folder
		// lives for the length of one run.
		if err := os.WriteFile(path, content, 0o644); err != nil { //nolint:gosec // the container user must read this
			_ = os.RemoveAll(dir)
			return "", fmt.Errorf("write %s: %w", filepath.Base(path), err)
		}
	}

	return dir, nil
}

// useInit is addressable because the Docker API wants a pointer.
var useInit = true

func (r *Runner) create(ctx context.Context, job Job, dir string) (string, error) {
	ttl := strconv.Itoa(int(r.cfg.TTL.Seconds()))

	// The time limit runs inside the container. A context deadline on this side
	// only helps while this process is alive; a worker killed mid-run leaves a
	// generator hammering someone's staging environment, and this is what stops
	// that.
	command := fmt.Sprintf(
		"timeout -s KILL %s k6 run --no-color %s/%s",
		ttl, scriptDir, entryName,
	)

	created, err := r.cli.ContainerCreate(ctx,
		&container.Config{
			Image:      r.cfg.Image,
			Entrypoint: []string{"sh", "-c", command},
			Env:        environ(job.Env),
			User:       containerUser,
			Labels:     map[string]string{RunIDLabel: job.RunID.String()},
		},
		&container.HostConfig{
			Mounts: []mount.Mount{
				{Type: mount.TypeBind, Source: filepath.Join(dir, "script"), Target: scriptDir, ReadOnly: true},
				{Type: mount.TypeBind, Source: filepath.Join(dir, "out"), Target: outDir},
			},
			// Nothing the script writes survives it, bar the summary.
			ReadonlyRootfs: true,
			Tmpfs:          map[string]string{"/tmp": "rw,size=64m,uid=12345,gid=12345"},
			CapDrop:        []string{"ALL"},
			SecurityOpt:    []string{"no-new-privileges"},
			ExtraHosts:     r.cfg.ExtraHosts,
			Resources: container.Resources{
				NanoCPUs:  int64(r.cfg.CPUs * 1e9),
				Memory:    r.cfg.MemoryMB * 1024 * 1024,
				PidsLimit: &r.cfg.PidsLimit,
			},
			// A real init process as PID 1, and it is not optional. Without it
			// the shell execs the time limit straight into PID 1, where it
			// stops enforcing anything and a run outlives its ceiling
			// silently. It also reaps whatever an uploaded script leaves
			// behind.
			Init: &useInit,

			// Removal is explicit, below. Letting the daemon do it would race
			// with reading the exit code.
			AutoRemove: false,
		},
		nil, nil, "",
	)
	if err != nil {
		return "", fmt.Errorf("create the container: %w", err)
	}

	return created.ID, nil
}

func (r *Runner) wait(ctx context.Context, id string) (int64, error) {
	done, errs := r.cli.ContainerWait(ctx, id, container.WaitConditionNotRunning)

	select {
	case status := <-done:
		if status.Error != nil {
			return 0, fmt.Errorf("wait for the container: %s", status.Error.Message)
		}
		return status.StatusCode, nil

	case err := <-errs:
		return 0, fmt.Errorf("wait for the container: %w", err)

	case <-ctx.Done():
		// The deferred removal takes the container down; reporting why comes
		// first.
		return 0, ctx.Err()
	}
}

// remove destroys the container, on a context of its own.
//
// Deriving it from the caller's would be the obvious thing and it is exactly
// wrong: a cancelled context means the cleanup is cancelled too, so the
// container survives precisely in the case that asked for it to die.
func (r *Runner) remove(ctx context.Context, id string) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()

	_ = r.cli.ContainerRemove(cleanup, id, container.RemoveOptions{
		Force:         true,
		RemoveVolumes: true,
	})
}

// environ renders the injected variables. The values are an environment's
// secrets; they are handed to Docker and never written anywhere else.
func environ(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}

	out := make([]string, 0, len(env))
	for key, value := range env {
		out = append(out, key+"="+value)
	}
	return out
}

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
	"github.com/docker/docker/api/types/network"
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

	// The name the generator reaches the proxy by, on the run's own network.
	proxyAlias = "egress"
	proxyPort  = "3128"
)

// DefaultProxyImage is the egress proxy, built by `make proxy-image` from
// deploy/proxy/Dockerfile. Not a registry tag: it is this repository's own code.
const DefaultProxyImage = "ballast/egress-proxy:dev"

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
	// so a development or test run can reach a server on the host machine.
	ExtraHosts []string
	// ProxyImage is the egress proxy, defaulting to DefaultProxyImage. It is
	// built from this repository rather than pulled, so a machine that has not
	// built it cannot run anything — which is the right failure: a run with no
	// proxy would be a run with no way out, or worse, one with an unguarded one.
	ProxyImage string
	// AllowedCIDRs names networks the proxy may reach although they are not
	// publicly routable, in CIDR form.
	//
	// **Empty in production, and it has to stay that way.** It exists for a run
	// launched on a developer's machine, whose target is on that machine, and
	// for the tests that predate the run network. Naming a private range here
	// lets a generator reach it.
	AllowedCIDRs []string
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
	if c.ProxyImage == "" {
		c.ProxyImage = DefaultProxyImage
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
	// AllowedHosts are the host:port pairs this run may reach. Everything else
	// is refused by the proxy, and unreachable by the network in any case.
	AllowedHosts []string
	// Network is the run network to use. Left empty, Run makes one and removes
	// it; given one, the caller owns its removal. Only the tests give one, so
	// that a target container can be attached to it before the run starts.
	Network string
}

func (j Job) validate() error {
	switch {
	case j.RunID == uuid.Nil:
		return fmt.Errorf("%w: no run id", ErrInvalidJob)
	case len(j.Script) == 0:
		return fmt.Errorf("%w: the scenario is empty", ErrInvalidJob)
	case len(j.Options) == 0:
		return fmt.Errorf("%w: no k6 options", ErrInvalidJob)
	case len(j.AllowedHosts) == 0:
		// A run that may reach nothing is a wiring mistake, not a run. Refused
		// here rather than started and left to fail every request, which looks
		// like a target being down.
		return fmt.Errorf("%w: no allowed hosts", ErrInvalidJob)
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
	// EgressBytes and IngressBytes are what crossed the socket to the target,
	// counted by the proxy. The generator's own figures are in its summary and
	// are smaller: they count what a script sent, not what left the machine.
	EgressBytes  int64
	IngressBytes int64
	// EgressMeasured says those two were read. False means they are zero
	// because nothing could be read — not because nothing moved. The
	// difference matters to whoever sums a month of traffic, so it travels
	// rather than being guessed at later.
	EgressMeasured bool
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

	// The run's own network, with no route out of it, and the proxy that is the
	// only thing on it with one. Both before the generator exists: a generator
	// on a network that can reach the internet is the thing this prevents, and
	// it must never exist even briefly.
	networkName := job.Network
	if networkName == "" {
		created, removeNetwork, err := r.NewRunNetwork(ctx, job.RunID)
		if err != nil {
			return Result{}, err
		}
		// Whoever makes a network removes it. A caller that supplied one owns
		// its removal instead, which only the tests do.
		defer removeNetwork()
		networkName = created
	}

	proxyID, err := r.startProxy(ctx, job, networkName)
	if err != nil {
		return Result{}, err
	}
	defer r.remove(ctx, proxyID)

	dir, err := r.writeScripts(job)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	id, err := r.create(ctx, job, dir, networkName)
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

	// Now that the generator is gone, the proxy has nothing left in flight:
	// its totals are final, and reading them is also what stops it.
	// Not fatal when it fails: a run that measured a target is still a run, and
	// what is lost is knowing what it cost. The caller is told, and says so in
	// its own log rather than this package inventing one.
	traffic, trafficErr := r.trafficOf(ctx, proxyID)

	// The path is built from the directory this runner created itself, not
	// from anything the job carried.
	summary, err := os.ReadFile(filepath.Join(dir, "out", "summary.json")) //nolint:gosec // path is ours
	if err != nil && !os.IsNotExist(err) {
		return Result{}, fmt.Errorf("read the summary: %w", err)
	}

	return Result{
		Summary:        summary,
		EgressBytes:    traffic.Sent,
		IngressBytes:   traffic.Received,
		EgressMeasured: trafficErr == nil,
		ExitCode:       exitCode,
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

func (r *Runner) create(ctx context.Context, job Job, dir, networkName string) (string, error) {
	ttl := strconv.Itoa(int(r.cfg.TTL.Seconds()))

	// The time limit runs inside the container. A context deadline on this side
	// only helps while this process is alive; a worker killed mid-run leaves a
	// generator hammering someone's staging environment, and this is what stops
	// that.
	// --no-usage-report because k6 otherwise calls stats.grafana.org on every
	// run. The proxy refuses it, so it was never going anywhere; what it left
	// was a misleading refusal in the log of every single run, and an attempt to
	// tell a third party how Ballast's customers use it.
	command := fmt.Sprintf(
		"timeout -s KILL %s k6 run --no-color --no-usage-report %s/%s",
		ttl, scriptDir, entryName,
	)

	created, err := r.cli.ContainerCreate(ctx,
		&container.Config{
			Image:      r.cfg.Image,
			Entrypoint: []string{"sh", "-c", command},
			Env:        environ(job.Env, proxyEnv()),
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

			// This network and no other. It has no gateway, so the generator
			// has no default route: the proxy is not the recommended way out,
			// it is the only one that exists.
			NetworkMode: container.NetworkMode(networkName),
		},
		&network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{
			networkName: {},
		}},
		nil, "",
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
//
// Ballast's own variables are written last, after the secrets, so that a secret
// cannot be named HTTPS_PROXY and point the generator at a way out of its own
// choosing. Storing such a key is refused where secrets are written; this is
// what holds if that check is ever widened.
func environ(secrets, ours map[string]string) []string {
	out := make([]string, 0, len(secrets)+len(ours))
	for key, value := range secrets {
		if _, taken := ours[key]; taken {
			continue
		}
		out = append(out, key+"="+value)
	}
	for key, value := range ours {
		out = append(out, key+"="+value)
	}

	return out
}

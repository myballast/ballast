// Command worker executes queued runs.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	// The zone database, inside the binary: the image this runs in has none, and
	// an environment's window is read in its own zone.
	_ "time/tzdata"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/adapter/k6"
	"github.com/youssefmahersi/ballast/internal/adapter/postgres"
	"github.com/youssefmahersi/ballast/internal/adapter/runner/hosted"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/infra"
	"github.com/youssefmahersi/ballast/internal/infra/config"
	"github.com/youssefmahersi/ballast/internal/infra/crypto"
	"github.com/youssefmahersi/ballast/internal/infra/logging"
	"github.com/youssefmahersi/ballast/internal/usecase/authz"
	"github.com/youssefmahersi/ballast/internal/usecase/run"
	fleet "github.com/youssefmahersi/ballast/internal/usecase/runner"
)

// version is the build, set at link time with -ldflags "-X main.version=<sha>".
// It is what the screen shows next to the runner; the contract is what is
// compared.
var version = "dev"

func main() {
	log := logging.New("worker")

	if err := work(log); err != nil {
		log.Error("the worker stopped", "error", err)
		os.Exit(1)
	}
}

func work(log *slog.Logger) error {
	cfg, err := config.LoadWorker()
	if err != nil {
		return err
	}

	// Two ways to stop, and they are not the same. The first signal is a
	// deployment: take nothing new and let the run that is measuring finish, so
	// that restarting this process is never a way of killing one. The second
	// signal, or the end of the time a run is allowed, is an order: cancel it,
	// which is what destroys its container.
	drain, stopDraining := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopDraining()

	hard, stopNow := context.WithCancel(context.Background())
	defer stopNow()
	go stopWhenTold(drain, stopNow, cfg.DrainTimeout, log)

	db, err := postgres.OpenSized(hard, cfg.DatabaseURL, cfg.PoolConnections())
	if err != nil {
		return err
	}
	defer db.Close()

	runner, err := hosted.New(hosted.Config{
		Image:        cfg.K6Image,
		TTL:          cfg.RunTTL,
		ExtraHosts:   cfg.ExtraHosts,
		AllowedCIDRs: cfg.EgressAllowedCIDRs,
		WorkDir:      cfg.WorkDir,
		ProxyImage:   cfg.ProxyImage,
	})
	if err != nil {
		return err
	}
	defer func() { _ = runner.Close() }()

	cipher, err := crypto.NewCipher(cfg.SecretKey)
	if err != nil {
		return err
	}

	service := run.NewService(
		postgres.NewRunRepository(db),
		postgres.NewScenarioRepository(db),
		postgres.NewEnvironmentRepository(db),
		postgres.NewEnvironmentRepository(db),
		postgres.NewSecretRepository(db, cipher),
		runner,
		// The same runner, in its other role: what starts a container is also
		// what knows how to find and destroy one somebody else started.
		runner,
		postgres.NewProjectRepository(db),
		// The worker appends the generator's output here; the API reads it back
		// for the live view.
		postgres.NewLogRepository(db),
		// The worker writes to the journal too: a run stopped on the doorstep
		// because its target was unverified since is a refusal, and refusals
		// are what the journal is for.
		postgres.NewAuditRepository(db),
		k6.NewOptionsBuilder(),
		k6.NewSummaryParser(),
		authz.NewService(postgres.NewMembershipRepository(db), log),
		infra.SystemClock{},
		run.Accepting(cfg.AcceptingRuns),
		log,
	)

	// Said before anything else is done, and from a goroutine of its own: the
	// worker is online from the moment it starts, through the sweep below, and
	// through a two-hour soak. The context is not the draining one, because a
	// draining worker is alive; the deferred call ends it only after the last
	// run is over, and before the database it writes to is closed.
	scope := serving(cfg, db)
	presence, err := fleet.NewPresence(
		postgres.NewRunnerRepository(db), infra.SystemClock{},
		scope, cfg.RunnerName, version, domain.RunnerContract)
	if err != nil {
		return err
	}
	presenceCtx, endPresence := context.WithCancel(context.Background())
	beating := make(chan struct{})
	go func() {
		defer close(beating)
		beat(presenceCtx, drain, presence, domain.RunnerHeartbeatEvery, log)
	}()
	defer func() {
		endPresence()
		<-beating
	}()

	log.Info("runner announced", "name", cfg.RunnerName, "version", version, "contract", domain.RunnerContract)
	if cfg.OrgID == uuid.Nil {
		log.Info("serving every organization",
			"max_concurrent_runs", cfg.MaxConcurrentRuns, "max_runs_per_org", cfg.MaxRunsPerOrg)
	} else {
		log.Info("serving one organization", "org_id", cfg.OrgID,
			"max_concurrent_runs", cfg.MaxConcurrentRuns, "max_runs_per_org", cfg.MaxRunsPerOrg)
	}

	if !cfg.AcceptingRuns {
		// Said once, loudly, at startup. A worker that silently takes nothing
		// looks exactly like a worker with nothing to do.
		log.Warn("this installation is not accepting new runs; the queue will not be drained",
			"setting", "BALLAST_ACCEPTING_RUNS")
	}

	// Before taking any work. A worker that was killed mid-run left a generator
	// behind, and a generator does not stop when its worker does: it keeps
	// loading somebody's target until its own time limit ends it.
	//
	// Every organization is asked about, not only the ones with work: this
	// daemon is this worker's alone, and what is on it belongs to a live run of
	// some organization or to nobody. When any list cannot be read, nothing is
	// destroyed.
	if everyone, err := scope.Present(hard); err != nil {
		log.Error("this machine could not be swept: the organizations could not be listed", "error", err)
	} else if destroyed, err := service.SweepOrganizations(hard, everyone); err != nil {
		// Loud, but not fatal. A daemon that cannot be swept is no reason to
		// stop executing runs.
		log.Error("this machine could not be swept", "error", err)
	} else if destroyed > 0 {
		log.Warn("containers left behind by an earlier worker were destroyed",
			"containers", destroyed)
	}

	// Under the hard context, not the draining one: a worker that is waiting for
	// its last run is still the one that has to notice another worker's death.
	go collect(hard, service, scope, cfg.CollectInterval, log)

	log.Info("polling for runs", "interval", cfg.PollInterval)

	// The service behind a guard: a worker older than the organization's minimum
	// stays up and takes nothing, and the run it did not take stays queued.
	guarded := &contractGuard{
		next:     service,
		gate:     fleet.NewGate(postgres.NewRunnerPolicyRepository(db), domain.RunnerContract),
		contract: domain.RunnerContract,
		log:      log,
	}

	slots, err := fleet.NewSlots(cfg.MaxConcurrentRuns, cfg.MaxRunsPerOrg)
	if err != nil {
		return err
	}

	return serve(drain, hard, guarded, scope, slots, cfg.PollInterval, log)
}

// serving is which organizations this worker serves: the one it was restricted
// to, or every one.
func serving(cfg config.Worker, db *postgres.DB) fleet.Scope {
	if cfg.OrgID != uuid.Nil {
		return fleet.OnlyOrganization(cfg.OrgID)
	}
	return fleet.AllOrganizations(postgres.NewOrganizationDirectory(db))
}

// stopWhenTold cancels the hard context once draining has begun and the time to
// drain has run out, or a second signal says not to wait.
func stopWhenTold(
	drain context.Context,
	stopNow context.CancelFunc,
	timeout time.Duration,
	log *slog.Logger,
) {
	<-drain.Done()

	// signal.NotifyContext keeps catching signals until it is stopped, so a
	// second one is delivered here as well.
	again := make(chan os.Signal, 1)
	signal.Notify(again, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(again)

	log.Info("draining: no new run will be taken, the one under way is left to finish",
		"wait_at_most", timeout)

	select {
	case <-again:
		log.Warn("a second signal: stopping the run under way")
	case <-time.After(timeout):
		log.Warn("the drain timed out: stopping the run under way", "waited", timeout)
	}
	stopNow()
}

// collect looks for runs whose worker has died, on its own schedule.
//
// A goroutine of its own rather than a step between two runs, and that is the
// point: a worker busy with a two-hour soak would otherwise collect nothing for
// two hours, which is precisely when another worker's death costs the most — a
// target held, and a generator loading it with nobody watching.
func collect(
	ctx context.Context,
	service *run.Service,
	scope Scope,
	every time.Duration,
	log *slog.Logger,
) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		// The organizations with a run in flight or abandoned: those are the
		// ones that can hold something to collect.
		orgs, err := scope.Active(ctx)
		if err != nil {
			log.Error("the organizations to collect for could not be listed", "error", err)
			continue
		}

		for _, orgID := range orgs {
			ended, err := service.Collect(ctx, orgID)
			if err != nil {
				// The next tick tries again, and the next organization is not
				// kept waiting by this one. A collector that stopped on its
				// first failure would be a collector that stops.
				log.Error("collecting abandoned runs failed", "org_id", orgID, "error", err)
				continue
			}
			if ended > 0 {
				log.Warn("runs abandoned by their worker were collected", "org_id", orgID, "runs", ended)
			}
		}
	}
}

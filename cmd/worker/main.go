// Command worker executes queued runs.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

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
)

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

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	runner, err := hosted.New(hosted.Config{
		Image:        cfg.K6Image,
		TTL:          cfg.RunTTL,
		ExtraHosts:   cfg.ExtraHosts,
		AllowedCIDRs: cfg.EgressAllowedCIDRs,
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
		k6.NewOptionsBuilder(),
		k6.NewSummaryParser(),
		authz.NewService(postgres.NewMembershipRepository(db), log),
		infra.SystemClock{},
		log,
	)

	// Before taking any work. A worker that was killed mid-run left a generator
	// behind, and a generator does not stop when its worker does: it keeps
	// loading somebody's target until its own time limit ends it.
	if destroyed, err := service.Sweep(ctx, cfg.OrgID); err != nil {
		// Loud, but not fatal. A daemon that cannot be swept is no reason to
		// stop executing runs.
		log.Error("this machine could not be swept", "error", err)
	} else if destroyed > 0 {
		log.Warn("containers left behind by an earlier worker were destroyed",
			"containers", destroyed)
	}

	go collect(ctx, service, cfg.OrgID, cfg.CollectInterval, log)

	log.Info("polling for runs", "org_id", cfg.OrgID, "interval", cfg.PollInterval)

	return poll(ctx, service, cfg.OrgID, cfg.PollInterval, log)
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
	orgID uuid.UUID,
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

		ended, err := service.Collect(ctx, orgID)
		if err != nil {
			// The next tick tries again. A collector that stopped on its first
			// failure would be a collector that stops.
			log.Error("collecting abandoned runs failed", "error", err)
			continue
		}
		if ended > 0 {
			log.Warn("runs abandoned by their worker were collected", "runs", ended)
		}
	}
}

// poll is the whole scheduling story for now: one organization, one run at a
// time, a fixed wait when there is nothing to do.
//
// It polls a single organization because row-level security means the
// application role cannot see, or even enumerate, any other. Finding work
// across tenants is an unsolved question that belongs with the queue.
func poll(
	ctx context.Context,
	service *run.Service,
	orgID uuid.UUID,
	interval time.Duration,
	log *slog.Logger,
) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		// Logs are drained and dropped here. Storing them is what the live log
		// view will do.
		logs := make(chan domain.LogLine, 256)
		drained := make(chan struct{})
		go func() {
			defer close(drained)
			for line := range logs {
				log.Debug("generator", "stream", line.Stream, "message", line.Message)
			}
		}()

		worked, err := service.ProcessNext(ctx, orgID, logs)
		close(logs)
		<-drained

		if err != nil {
			// A failed run must not take the worker down with it.
			log.Error("processing a run failed", "error", err)
		}

		if worked {
			continue
		}

		select {
		case <-ctx.Done():
			log.Info("stopping")
			return nil
		case <-ticker.C:
		}
	}
}

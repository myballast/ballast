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
		Image:      cfg.K6Image,
		TTL:        cfg.RunTTL,
		ExtraHosts: cfg.ExtraHosts,
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
		postgres.NewSecretRepository(db, cipher),
		runner,
		k6.NewOptionsBuilder(),
		k6.NewSummaryParser(),
		authz.NewService(postgres.NewMembershipRepository(db), log),
		infra.SystemClock{},
		log,
	)

	log.Info("polling for runs", "org_id", cfg.OrgID, "interval", cfg.PollInterval)

	return poll(ctx, service, cfg.OrgID, cfg.PollInterval, log)
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

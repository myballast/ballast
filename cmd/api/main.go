// Command api serves Ballast's HTTP API.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/youssefmahersi/ballast/internal/adapter/k6"
	"github.com/youssefmahersi/ballast/internal/adapter/postgres"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/infra"
	"github.com/youssefmahersi/ballast/internal/infra/config"
	"github.com/youssefmahersi/ballast/internal/infra/logging"
	"github.com/youssefmahersi/ballast/internal/usecase/run"

	httpadapter "github.com/youssefmahersi/ballast/internal/adapter/http"
)

func main() {
	log := logging.New("api")

	if err := serve(log); err != nil {
		log.Error("the api stopped", "error", err)
		os.Exit(1)
	}
}

// serve is where every layer meets. It is the only place that knows about all
// of them at once.
func serve(log *slog.Logger) error {
	cfg, err := config.Load()
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

	// The API never executes anything, so it is handed an executor that says
	// so rather than a Docker connection it would not use.
	service := run.NewService(
		postgres.NewRunRepository(db),
		postgres.NewScenarioRepository(db),
		postgres.NewEnvironmentRepository(db),
		refusingExecutor{},
		k6.NewOptionsBuilder(),
		k6.NewSummaryParser(),
		infra.SystemClock{},
		log,
	)

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           httpadapter.NewRouter(service, cfg.DevOrgID, log),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()

		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()

		_ = server.Shutdown(shutdown)
	}()

	log.Info("listening", "addr", cfg.Addr, "org_id", cfg.DevOrgID)

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// refusingExecutor stands in for the generator in the API process.
//
// The API accepts runs, it never executes one, so handing it a Docker
// connection would be giving it a capability it has no business holding. If
// this is ever called, something is wired wrong, and saying so loudly beats a
// nil pointer.
type refusingExecutor struct{}

func (refusingExecutor) Execute(
	context.Context, run.Job, chan<- domain.LogLine,
) (run.Outcome, error) {
	return run.Outcome{}, errors.New("the api does not execute runs; that is the worker's job")
}

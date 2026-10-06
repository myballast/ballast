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

	"github.com/youssefmahersi/ballast/internal/adapter/http/session"
	"github.com/youssefmahersi/ballast/internal/adapter/k6"
	"github.com/youssefmahersi/ballast/internal/adapter/postgres"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/infra"
	"github.com/youssefmahersi/ballast/internal/infra/config"
	"github.com/youssefmahersi/ballast/internal/infra/logging"
	"github.com/youssefmahersi/ballast/internal/infra/oidc"
	"github.com/youssefmahersi/ballast/internal/usecase/auth"
	"github.com/youssefmahersi/ballast/internal/usecase/authz"
	"github.com/youssefmahersi/ballast/internal/usecase/project"
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
	cfg, err := config.LoadAPI()
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

	memberships := postgres.NewMembershipRepository(db)
	permissions := authz.NewService(memberships, log)

	projects := project.NewService(
		postgres.NewProjectRepository(db),
		memberships,
		permissions,
		infra.SystemClock{},
		log,
	)

	sessions, err := session.NewManager(cfg.SessionSecret, cfg.SessionTTL, infra.SystemClock{})
	if err != nil {
		return err
	}

	signIn := auth.NewService(
		postgres.NewAccountRepository(db),
		providers(cfg),
		infra.SystemClock{},
		log,
	)
	if len(signIn.Providers()) == 0 {
		log.Warn("no identity provider is configured; nobody can sign in",
			"hint", "set BALLAST_GITHUB_CLIENT_ID and BALLAST_GITHUB_CLIENT_SECRET")
	}

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           httpadapter.NewRouter(service, projects, signIn, sessions, cfg.WebURL, log),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()

		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()

		_ = server.Shutdown(shutdown)
	}()

	log.Info("listening", "addr", cfg.Addr, "providers", signIn.Providers())

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// providers builds the identity providers that have credentials. Each one's
// callback is on this API, at the address browsers reach it by.
func providers(cfg config.API) map[string]auth.IdentityProvider {
	callback := func(name string) string {
		return cfg.PublicURL.JoinPath("auth", name, "callback").String()
	}

	enabled := map[string]auth.IdentityProvider{}
	if cfg.GitHub.Enabled() {
		enabled["github"] = oidc.NewGitHub(cfg.GitHub.ID, cfg.GitHub.Secret, callback("github"))
	}
	if cfg.Google.Enabled() {
		enabled["google"] = oidc.NewGoogle(cfg.Google.ID, cfg.Google.Secret, callback("google"))
	}
	return enabled
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

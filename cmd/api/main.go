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

	// The time zone database, carried in the binary. An allowed window is
	// resolved in its own zone, and this process ships in an image that holds
	// nothing but this binary — without it, time.LoadLocation fails and every
	// environment carrying a window is refused. In production only: a
	// developer's machine has the zones on disk.
	_ "time/tzdata"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/adapter/http/session"
	"github.com/youssefmahersi/ballast/internal/adapter/k6"
	"github.com/youssefmahersi/ballast/internal/adapter/openapi"
	"github.com/youssefmahersi/ballast/internal/adapter/postgres"
	"github.com/youssefmahersi/ballast/internal/adapter/verifier"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/infra"
	"github.com/youssefmahersi/ballast/internal/infra/config"
	"github.com/youssefmahersi/ballast/internal/infra/crypto"
	"github.com/youssefmahersi/ballast/internal/infra/logging"
	"github.com/youssefmahersi/ballast/internal/infra/oidc"
	"github.com/youssefmahersi/ballast/internal/usecase/audit"
	"github.com/youssefmahersi/ballast/internal/usecase/auth"
	"github.com/youssefmahersi/ballast/internal/usecase/authz"
	"github.com/youssefmahersi/ballast/internal/usecase/catalog"
	"github.com/youssefmahersi/ballast/internal/usecase/environment"
	"github.com/youssefmahersi/ballast/internal/usecase/invitation"
	"github.com/youssefmahersi/ballast/internal/usecase/project"
	"github.com/youssefmahersi/ballast/internal/usecase/run"
	"github.com/youssefmahersi/ballast/internal/usecase/scenario"

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

	memberships := postgres.NewMembershipRepository(db)
	permissions := authz.NewService(memberships, log)

	// One journal, written by three services: who launched or was refused a
	// run, who proved and approved a target, who changed somebody's role.
	journal := postgres.NewAuditRepository(db)

	cipher, err := crypto.NewCipher(cfg.SecretKey)
	if err != nil {
		return err
	}
	secrets := postgres.NewSecretRepository(db, cipher)

	// The API never executes anything, so it is handed an executor that says
	// so rather than a Docker connection it would not use.
	service := run.NewService(
		postgres.NewRunRepository(db),
		postgres.NewScenarioRepository(db),
		postgres.NewEnvironmentRepository(db),
		postgres.NewEnvironmentRepository(db),
		// The API accepts runs and never executes one, so it is given a reader
		// that refuses rather than the one that can decrypt a secret.
		refusingSecrets{},
		refusingExecutor{},
		refusingContainers{},
		// Read by the emergency stop alone, to ask the authorizer about every
		// project of the organization.
		postgres.NewProjectRepository(db),
		// The worker appends the generator's output here; the API reads it back
		// for the live view.
		postgres.NewLogRepository(db),
		journal,
		k6.NewOptionsBuilder(),
		k6.NewSummaryParser(),
		permissions,
		infra.SystemClock{},
		// The platform's switch. Off, POST /runs answers 503 and nothing is
		// written; the worker reads the same setting and leaves the queue alone.
		run.Accepting(cfg.AcceptingRuns),
		log,
	)

	projects := project.NewService(
		postgres.NewProjectRepository(db),
		memberships,
		permissions,
		journal,
		infra.SystemClock{},
		log,
	)

	scenarios := scenario.NewService(
		postgres.NewScenarioWriteRepository(db),
		postgres.NewScenarioWriteRepository(db),
		k6.NewScriptInspector(),
		openapi.NewReader(),
		k6.NewGenerator(),
		permissions,
		infra.SystemClock{},
		log,
	)

	catalogs := catalog.NewService(
		postgres.NewCatalogRepository(db),
		openapi.NewReader(),
		permissions,
		infra.SystemClock{},
		log,
	)

	environments := environment.NewService(
		postgres.NewEnvironmentWriteRepository(db),
		secrets,
		crypto.NewRandomTokens(),
		// The one component that makes an outbound request to an address
		// somebody else chose. It refuses anything that does not resolve
		// publicly, which is what keeps the endpoint from being a way to read
		// Ballast's own network.
		verifier.New(),
		journal,
		permissions,
		infra.SystemClock{},
		log,
	)

	accounts := postgres.NewAccountRepository(db)

	invitations := invitation.NewService(
		postgres.NewInvitationRepository(db),
		accounts,
		memberships,
		permissions,
		journal,
		crypto.NewRandomTokens(),
		infra.SystemClock{},
		log,
	)

	journalReader := audit.NewService(journal, permissions, log)

	sessions, err := session.NewManager(cfg.SessionSecret, cfg.SessionTTL, infra.SystemClock{})
	if err != nil {
		return err
	}

	signIn := auth.NewService(
		accounts,
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
		Handler:           httpadapter.NewRouter(service, projects, scenarios, catalogs, environments, invitations, journalReader, signIn, permissions, sessions, db.Ping, cfg.WebURL, log),
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

// refusingContainers stands in for the garbage collector in the API process.
//
// Collecting abandoned runs means destroying containers, which is the worker's
// machine and the worker's business. The API is given a Docker connection
// nowhere, so there is nothing here to refuse with but an error.
type refusingContainers struct{}

func (refusingContainers) Destroy(context.Context, uuid.UUID) error {
	return errors.New("the api destroys no containers; that is the worker's job")
}

func (refusingContainers) DestroyExcept(context.Context, []uuid.UUID) (int, error) {
	return 0, errors.New("the api destroys no containers; that is the worker's job")
}

// refusingSecrets stands in for the secret reader in the API process.
//
// The API writes secrets, through a port that has no method for reading one back.
// It is wired to this rather than to the repository that can decrypt, so that the
// capability is absent from the process rather than merely unused by it.
type refusingSecrets struct{}

func (refusingSecrets) SecretsOf(
	context.Context, uuid.UUID, uuid.UUID,
) (map[string]string, error) {
	return nil, errors.New("the api does not decrypt secrets; that is the worker's job")
}

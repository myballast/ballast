package http

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/youssefmahersi/ballast/internal/adapter/http/handler"
	"github.com/youssefmahersi/ballast/internal/adapter/http/middleware"
	"github.com/youssefmahersi/ballast/internal/adapter/http/session"
	"github.com/youssefmahersi/ballast/internal/usecase/audit"
	"github.com/youssefmahersi/ballast/internal/usecase/auth"
	"github.com/youssefmahersi/ballast/internal/usecase/environment"
	"github.com/youssefmahersi/ballast/internal/usecase/invitation"
	"github.com/youssefmahersi/ballast/internal/usecase/project"
	"github.com/youssefmahersi/ballast/internal/usecase/run"
	"github.com/youssefmahersi/ballast/internal/usecase/scenario"
)

// Probe answers whether what the API depends on can be reached. It is a
// function rather than an interface because there is one of them, and because
// this package has no business knowing a database driver: cmd/api closes it
// over the pool.
type Probe func(context.Context) error

// readyTimeout bounds the readiness check. A platform polls this endpoint, and
// one that hangs on an unreachable database is worse than one that answers no:
// the first looks like an API that is merely slow.
const readyTimeout = 2 * time.Second

// NewRouter wires the HTTP surface.
//
// net/http's own multiplexer, not a third-party router: since Go 1.22 it
// matches on method and path pattern, which is the whole of what this API needs.
//
// Nothing is registered here by hand. Every route comes from the table in
// routes.go, which carries the permission it requires, and a test reads the
// source of this package to refuse any registration made another way. That is
// what makes it impossible to add a route without saying what it needs.
//
// A route declared with no handler, or a handler with no declaration, panics on
// startup. Both are wiring mistakes, and a process that refuses to start is a
// better outcome than an endpoint that silently answers nothing — or worse,
// answers without being checked.
func NewRouter(
	runs *run.Service,
	projects *project.Service,
	scenarios *scenario.Service,
	environments *environment.Service,
	invitations *invitation.Service,
	journal *audit.Service,
	signIn *auth.Service,
	permissions middleware.Authorizer,
	sessions *session.Manager,
	ready Probe,
	web *url.URL,
	log *slog.Logger,
) http.Handler {
	runHandler := handler.NewRun(runs, log)
	projectHandler := handler.NewProject(projects, log)
	scenarioHandler := handler.NewScenario(scenarios, log)
	environmentHandler := handler.NewEnvironment(environments, log)
	invitationHandler := handler.NewInvitation(invitations, sessions, log)
	auditHandler := handler.NewAudit(journal, log)
	authHandler := handler.NewAuth(signIn, sessions, web, log)

	handlers := map[string]http.HandlerFunc{
		"GET /healthz": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok\n"))
		},
		// Liveness says the process is up; this says it can do its work. A
		// platform that restarts on a failed liveness check would otherwise
		// restart a healthy API because its database was briefly away.
		"GET /readyz": func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
			defer cancel()

			if err := ready(ctx); err != nil {
				// The reason stays in the log. What is handed to whoever
				// called is that the answer is no: a driver's error names the
				// host, the user and the database it was given.
				log.WarnContext(ctx, "the readiness probe could not reach the database", "error", err)
				http.Error(w, "not ready\n", http.StatusServiceUnavailable)
				return
			}

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ready\n"))
		},
		"GET /auth/providers":           authHandler.Providers,
		"GET /auth/{provider}/login":    authHandler.Login,
		"GET /auth/{provider}/callback": authHandler.Callback,
		"POST /auth/logout":             authHandler.Logout,
		"GET /me":                       authHandler.Me,
		"POST /me/organization":         authHandler.SwitchOrganization,

		"GET /roles":                       projectHandler.Roles,
		"GET /invitations/{token}":         invitationHandler.Describe,
		"POST /invitations/{token}/accept": invitationHandler.Accept,

		"POST /runs":            runHandler.Enqueue,
		"GET /runs/{id}":        runHandler.ByID,
		"DELETE /runs/{id}":     runHandler.Cancel,
		"POST /runs/{id}/rerun": runHandler.Rerun,
		"GET /runs/{id}/logs":   runHandler.Logs,
		"POST /runs/stop-all":   runHandler.StopAll,

		"GET /projects":                                           projectHandler.List,
		"POST /projects":                                          projectHandler.Create,
		"GET /projects/{projectId}":                               projectHandler.ByID,
		"PATCH /projects/{projectId}":                             projectHandler.Update,
		"DELETE /projects/{projectId}":                            projectHandler.Delete,
		"GET /usage":                                              runHandler.Usage,
		"GET /projects/{projectId}/usage":                         runHandler.ProjectUsage,
		"GET /projects/{projectId}/audit":                         auditHandler.InProject,
		"GET /projects/{projectId}/members":                       projectHandler.Members,
		"GET /projects/{projectId}/invitations":                   invitationHandler.List,
		"POST /projects/{projectId}/invitations":                  invitationHandler.Create,
		"DELETE /projects/{projectId}/invitations/{invitationId}": invitationHandler.Revoke,
		"GET /projects/{projectId}/permissions":                   projectHandler.Permissions,
		"PUT /projects/{projectId}/favorite":                      projectHandler.Favorite,
		"DELETE /projects/{projectId}/favorite":                   projectHandler.Unfavorite,
		"PUT /projects/{projectId}/members/{userId}":              projectHandler.SetMemberRole,
		"DELETE /projects/{projectId}/members/{userId}":           projectHandler.RemoveMember,

		"GET /projects/{projectId}/scenarios":                                          scenarioHandler.List,
		"POST /projects/{projectId}/scenarios":                                         scenarioHandler.Create,
		"GET /projects/{projectId}/scenarios/{scenarioId}":                             scenarioHandler.ByID,
		"PATCH /projects/{projectId}/scenarios/{scenarioId}":                           scenarioHandler.Update,
		"DELETE /projects/{projectId}/scenarios/{scenarioId}":                          scenarioHandler.Delete,
		"GET /projects/{projectId}/scenarios/{scenarioId}/versions":                    scenarioHandler.Versions,
		"GET /projects/{projectId}/scenarios/{scenarioId}/thresholds":                  scenarioHandler.Thresholds,
		"POST /projects/{projectId}/scenarios/{scenarioId}/thresholds":                 scenarioHandler.AddThreshold,
		"PATCH /projects/{projectId}/scenarios/{scenarioId}/thresholds/{thresholdId}":  scenarioHandler.UpdateThreshold,
		"DELETE /projects/{projectId}/scenarios/{scenarioId}/thresholds/{thresholdId}": scenarioHandler.RemoveThreshold,

		"GET /projects/{projectId}/runs":                                          runHandler.List,
		"GET /projects/{projectId}/stats":                                         runHandler.Stats,
		"GET /projects/{projectId}/environments":                                  environmentHandler.List,
		"POST /projects/{projectId}/environments":                                 environmentHandler.Create,
		"GET /projects/{projectId}/environments/{environmentId}":                  environmentHandler.ByID,
		"PATCH /projects/{projectId}/environments/{environmentId}":                environmentHandler.Update,
		"DELETE /projects/{projectId}/environments/{environmentId}":               environmentHandler.Delete,
		"GET /projects/{projectId}/environments/{environmentId}/secrets":          environmentHandler.Secrets,
		"PUT /projects/{projectId}/environments/{environmentId}/secrets/{key}":    environmentHandler.SetSecret,
		"DELETE /projects/{projectId}/environments/{environmentId}/secrets/{key}": environmentHandler.RemoveSecret,

		"GET /projects/{projectId}/environments/{environmentId}/verification": environmentHandler.Challenge,
		"POST /projects/{projectId}/environments/{environmentId}/verify":      environmentHandler.Verify,
		"POST /projects/{projectId}/environments/{environmentId}/approve":     environmentHandler.Approve,
	}

	authenticate := middleware.Authenticate(sessions, log)

	mux := http.NewServeMux()
	for _, route := range Routes() {
		pattern := route.Method + " " + route.Path

		next, ok := handlers[pattern]
		if !ok {
			panic(fmt.Sprintf("http: %s is declared in routes.go with no handler", pattern))
		}
		delete(handlers, pattern)

		mux.Handle(pattern, wrap(route, http.Handler(next), authenticate, permissions, log))
	}

	for pattern := range handlers {
		panic(fmt.Sprintf("http: %s has a handler and is not declared in routes.go", pattern))
	}

	return mux
}

// wrap puts a route behind what its scope calls for.
func wrap(
	route Route,
	next http.Handler,
	authenticate func(http.Handler) http.Handler,
	permissions middleware.Authorizer,
	log *slog.Logger,
) http.Handler {
	if route.Scope == ScopePublic {
		return next
	}

	// The project sits in the path, so the declaration is enforced here rather
	// than left to the service to remember.
	if route.Scope == ScopePath {
		next = middleware.RequirePermission(permissions, route.Permission, route.ProjectParam, log)(next)
	}

	return authenticate(next)
}

package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/youssefmahersi/ballast/internal/adapter/http/session"
	"github.com/youssefmahersi/ballast/internal/domain"
)

// TokenAuthenticator says who is behind an API token. Declared here, by the code
// that consumes it, and implemented by usecase/apitoken.
type TokenAuthenticator interface {
	// Authenticate returns the identity of the token, or an error that matches
	// domain.ErrUnauthenticated for any refusal. Any other error is a failure
	// of Ballast and not of the credential.
	Authenticate(ctx context.Context, secret string) (domain.Identity, error)
}

// bearerToken reads the secret out of an Authorization header, and whether the
// request carries one at all.
//
// present is true as soon as there is an Authorization header, whatever it says;
// ok is true only for exactly one header of the form "Bearer <token>". The scheme
// is compared without regard to case, as the standard says. A token is read from
// this header and from nowhere else: one in a URL ends up in access logs,
// proxies and browser history.
func bearerToken(r *http.Request) (secret string, present, ok bool) {
	values := r.Header.Values("Authorization")
	if len(values) == 0 {
		return "", false, false
	}
	if len(values) > 1 {
		// Two credentials are an ambiguity, not a vote.
		return "", true, false
	}

	scheme, rest, found := strings.Cut(values[0], " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || rest == "" || strings.ContainsAny(rest, " \t") {
		return "", true, false
	}
	return rest, true, true
}

// AuthenticateOrToken lets a request through with a valid session or a valid
// bearer token, and puts who sent it in its context. It is for the routes that
// opted in to tokens; every other route uses Authenticate, which does not know
// what a token is — which is what makes the list of routes a list.
//
// A request that carries an Authorization header is judged by it alone. A good
// session cookie next to a bad token is two credentials that disagree, and
// quietly choosing the one that works is how a revoked token keeps working for
// somebody who happens to be signed in. Without the header, the session decides.
//
// Every refusal is the same 401. A failure of Ballast itself is a 503: a
// database that is down must not read as a wrong credential, or every pipeline
// would be told to rotate a good token in the middle of an outage.
func AuthenticateOrToken(
	sessions *session.Manager, tokens TokenAuthenticator, log *slog.Logger,
) func(http.Handler) http.Handler {
	sessionOnly := Authenticate(sessions, log)

	return func(next http.Handler) http.Handler {
		withSession := sessionOnly(next)

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			secret, present, ok := bearerToken(r)
			if !present {
				withSession.ServeHTTP(w, r)
				return
			}
			if !ok {
				log.DebugContext(r.Context(), "unauthenticated request: not a bearer token", "path", r.URL.Path)
				refuse(w, http.StatusUnauthorized, "sign in to continue")
				return
			}

			who, err := tokens.Authenticate(r.Context(), secret)
			switch {
			case errors.Is(err, domain.ErrUnauthenticated):
				refuse(w, http.StatusUnauthorized, "sign in to continue")
				return
			case err != nil:
				// The cause stays in the log: it can name a host, a user or a
				// database. The secret is not in it, and is not repeated here.
				log.ErrorContext(r.Context(), "could not authenticate an API token", "path", r.URL.Path, "error", err)
				refuse(w, http.StatusServiceUnavailable, "authentication is unavailable, try again")
				return
			}

			next.ServeHTTP(w, r.WithContext(session.WithIdentity(r.Context(), who)))
		})
	}
}

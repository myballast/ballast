package middleware

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

// OriginOf is what a browser puts in the Origin header for a page at u: the
// scheme, the host and the port, with nothing after, and no port that is the
// scheme's own.
func OriginOf(u *url.URL) string {
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Host)

	switch {
	case scheme == "https" && strings.HasSuffix(host, ":443"):
		host = strings.TrimSuffix(host, ":443")
	case scheme == "http" && strings.HasSuffix(host, ":80"):
		host = strings.TrimSuffix(host, ":80")
	}

	return scheme + "://" + host
}

// OriginGuard refuses a request that a browser sent from a page of another
// site, and answers the front end's own as cross-origin.
//
// A request with no Origin is not from a page: it is the front end's server, or
// a command line, and whether it may do anything is the session's business.
// A request with one is from a browser, and the browser says whose page it is:
// anything but the allowed origins is refused before the handler runs, whatever
// the method. The cookie being SameSite=Lax already keeps a forged POST from
// carrying it; this is the second lock, and the one a GET with side effects, or
// a route added carelessly tomorrow, would otherwise leave open.
//
// allowed holds origins as OriginOf writes them. They are compared exactly: a
// host that merely starts the same, another port, or http for https is another
// origin. "null", which a sandboxed page sends, is never one of them.
func OriginGuard(allowed []string, log *slog.Logger) func(http.Handler) http.Handler {
	known := make(map[string]bool, len(allowed))
	for _, origin := range allowed {
		known[origin] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			// What differs by origin must not be cached for another.
			w.Header().Add("Vary", "Origin")

			if !known[origin] {
				log.WarnContext(r.Context(), "a request from an origin that is not allowed",
					"origin", truncate(origin, 100), "method", r.Method, "path", r.URL.Path)
				// What the caller sent is theirs; it is not repeated back.
				refuse(w, http.StatusForbidden, "this origin is not allowed")
				return
			}

			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")

			if r.Method == http.MethodOptions {
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Last-Event-ID")
				w.Header().Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

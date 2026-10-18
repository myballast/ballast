package handler

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/adapter/http/dto"
	"github.com/youssefmahersi/ballast/internal/adapter/http/session"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/auth"
)

// FlowCookieName holds what the callback needs to check that it answers a
// sign-in this browser started.
const FlowCookieName = "ballast_oauth"

// A sign-in that takes longer than this starts over.
const flowLifetime = 10 * time.Minute

// Auth maps the sign-in flow onto the auth service.
//
// The flow is OAuth2's authorization code with PKCE. Login stores a random
// state and verifier in a short-lived cookie and sends the browser to the
// provider; the callback checks that the state the provider echoes is the one
// in the cookie, so that a callback this browser did not start — a forged
// link signing the victim into the attacker's account — is refused.
type Auth struct {
	auth     *auth.Service
	sessions *session.Manager
	web      *url.URL
	log      *slog.Logger
}

// NewAuth returns the handler. web is the front end, where every sign-in ends.
func NewAuth(service *auth.Service, sessions *session.Manager, web *url.URL, log *slog.Logger) *Auth {
	return &Auth{auth: service, sessions: sessions, web: web, log: log}
}

// Providers handles GET /auth/providers.
func (h *Auth) Providers(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, dto.ProvidersResponse{Providers: h.auth.Providers()})
}

// Login handles GET /auth/{provider}/login.
func (h *Auth) Login(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")

	state, verifier := randomToken(), randomToken()

	target, err := h.auth.LoginURL(provider, state, verifier)
	if errors.Is(err, auth.ErrUnknownProvider) {
		writeError(w, http.StatusNotFound, "no such identity provider")
		return
	}
	if err != nil {
		h.log.ErrorContext(r.Context(), "start a sign-in", "provider", provider, "error", err)
		writeError(w, http.StatusInternalServerError, "something went wrong")
		return
	}

	next := safeNext(r.URL.Query().Get("next"))
	http.SetCookie(w, flowCookie(encodeFlow(flow{provider, state, verifier, next}), int(flowLifetime.Seconds())))
	http.Redirect(w, r, target, http.StatusFound)
}

// Callback handles GET /auth/{provider}/callback.
//
// Every failure sends the browser back to the login page with a short code,
// never with the provider's or the database's own message.
func (h *Auth) Callback(w http.ResponseWriter, r *http.Request) {
	// One use only, whatever happens next.
	http.SetCookie(w, flowCookie("", -1))

	provider := r.PathValue("provider")
	query := r.URL.Query()

	started, ok := h.readFlow(r)
	if !ok || started.provider != provider || !sameToken(started.state, query.Get("state")) {
		h.log.WarnContext(r.Context(), "sign-in callback does not match a sign-in this browser started", "provider", provider)
		h.toLogin(w, r, "invalid_request")
		return
	}

	if reason := query.Get("error"); reason != "" {
		h.log.InfoContext(r.Context(), "sign-in refused at the provider", "provider", provider, "reason", reason)
		h.toLogin(w, r, "denied")
		return
	}

	code := query.Get("code")
	if code == "" {
		h.toLogin(w, r, "invalid_request")
		return
	}

	who, err := h.auth.Complete(r.Context(), provider, code, started.verifier)
	if err != nil {
		h.log.WarnContext(r.Context(), "sign-in failed", "provider", provider, "error", err)
		h.toLogin(w, r, "signin_failed")
		return
	}

	if err := h.sessions.Issue(w, who); err != nil {
		h.log.ErrorContext(r.Context(), "issue a session", "error", err)
		h.toLogin(w, r, "signin_failed")
		return
	}

	// safeNext has already ruled out a scheme or a host, so resolving it can
	// only land on the front end.
	next, err := url.Parse(started.next)
	if err != nil {
		next = &url.URL{Path: "/"}
	}
	http.Redirect(w, r, h.web.ResolveReference(next).String(), http.StatusFound)
}

// Logout handles POST /auth/logout. A POST, so that a link or an image on
// somebody else's page cannot sign a person out.
func (h *Auth) Logout(w http.ResponseWriter, _ *http.Request) {
	h.sessions.Clear(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Auth) toLogin(w http.ResponseWriter, r *http.Request, reason string) {
	target := h.web.JoinPath("login")
	target.RawQuery = url.Values{"error": {reason}}.Encode()
	http.Redirect(w, r, target.String(), http.StatusFound)
}

func (h *Auth) readFlow(r *http.Request) (flow, bool) {
	c, err := r.Cookie(FlowCookieName)
	if err != nil {
		return flow{}, false
	}
	return decodeFlow(c.Value)
}

// flow is what Login hands to Callback, through the browser.
type flow struct {
	provider, state, verifier, next string
}

// The parts are joined with dots, which neither a provider name nor a
// base64url token can contain. next is encoded so that it cannot either.
func encodeFlow(f flow) string {
	return strings.Join([]string{
		f.provider, f.state, f.verifier,
		base64.RawURLEncoding.EncodeToString([]byte(f.next)),
	}, ".")
}

func decodeFlow(value string) (flow, bool) {
	parts := strings.Split(value, ".")
	if len(parts) != 4 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return flow{}, false
	}
	next, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil {
		return flow{}, false
	}
	return flow{parts[0], parts[1], parts[2], safeNext(string(next))}, true
}

// flowCookie is only sent back to /auth/, and only on a top-level navigation
// from another site — which is exactly what the provider's redirect is.
func flowCookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     FlowCookieName,
		Value:    value,
		Path:     "/auth/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}
}

// safeNext keeps a post-sign-in destination to a path on the front end.
// Anything that could name another host — a scheme, a leading //, a backslash
// some browsers read as a slash — falls back to the home page.
func safeNext(next string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.Contains(next, "\\") {
		return "/"
	}
	parsed, err := url.Parse(next)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" {
		return "/"
	}
	return next
}

// randomToken returns 256 random bits, base64url-encoded: 43 characters, which
// is also the shortest PKCE verifier allowed.
func randomToken() string {
	b := make([]byte, 32)
	// crypto/rand.Read does not fail on any supported platform; if it ever
	// did, carrying on with a predictable state would be the wrong answer.
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func sameToken(a, b string) bool {
	return a != "" && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// Me handles GET /me.
func (h *Auth) Me(w http.ResponseWriter, r *http.Request) {
	who, ok := signedIn(w, r)
	if !ok {
		return
	}

	me, err := h.auth.Me(r.Context(), who)
	if errors.Is(err, domain.ErrUnauthenticated) {
		writeError(w, http.StatusUnauthorized, "sign in to continue")
		return
	}
	if err != nil {
		h.log.ErrorContext(r.Context(), "read the signed-in user", "error", err)
		writeError(w, http.StatusInternalServerError, "something went wrong")
		return
	}

	organizations, err := h.auth.Organizations(r.Context(), who)
	if err != nil {
		h.log.ErrorContext(r.Context(), "list the organizations", "error", err)
		writeError(w, http.StatusInternalServerError, "something went wrong")
		return
	}

	listed := make([]dto.MeOrganization, 0, len(organizations))
	for _, organization := range organizations {
		listed = append(listed, dto.MeOrganization{ID: organization.ID.String(), Name: organization.Name})
	}

	writeJSON(w, http.StatusOK, dto.MeResponse{
		User:          dto.MeUser{ID: me.UserID.String(), Name: me.Name, Email: me.Email},
		Organization:  dto.MeOrganization{ID: me.OrgID.String(), Name: me.OrgName},
		Organizations: listed,
	})
}

// SwitchOrganization handles POST /me/organization: the same person, acting for
// another organization they belong to. The answer is a new session.
func (h *Auth) SwitchOrganization(w http.ResponseWriter, r *http.Request) {
	who, ok := signedIn(w, r)
	if !ok {
		return
	}

	var body dto.SwitchOrganizationRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "the request body is not valid JSON")
		return
	}
	orgID, err := uuid.Parse(body.OrganizationID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "the organization id is not a uuid")
		return
	}

	switched, err := h.auth.SwitchOrganization(r.Context(), who, orgID)
	if errors.Is(err, auth.ErrNotAMember) {
		writeError(w, http.StatusForbidden, "you do not belong to that organization")
		return
	}
	if errors.Is(err, domain.ErrUnauthenticated) {
		writeError(w, http.StatusUnauthorized, "sign in to continue")
		return
	}
	if err != nil {
		h.log.ErrorContext(r.Context(), "switch organization", "error", err)
		writeError(w, http.StatusInternalServerError, "something went wrong")
		return
	}

	if err := h.sessions.Issue(w, switched); err != nil {
		h.log.ErrorContext(r.Context(), "issue a session", "error", err)
		writeError(w, http.StatusInternalServerError, "something went wrong")
		return
	}

	h.log.InfoContext(r.Context(), "organization switched", "user_id", switched.UserID, "org_id", switched.OrgID)
	w.WriteHeader(http.StatusNoContent)
}

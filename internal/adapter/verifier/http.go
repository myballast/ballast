package verifier

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// What this package refuses, as sentinels, because the four are fixed four
// different ways: put the file there, correct its contents, give the target a
// publicly reachable address, stop redirecting.
var (
	// ErrProofMissing reports that the target answered, and not with the file.
	ErrProofMissing = errors.New("the challenge file was not served")
	// ErrProofMismatch reports a file that is not the token. A great many
	// servers answer 200 to everything, so the contents are what decide.
	ErrProofMismatch = errors.New("the challenge file does not hold the expected token")
	// ErrAddressRefused reports a target that resolves somewhere Ballast will
	// not connect: its own network, or the cloud metadata service.
	ErrAddressRefused = errors.New("the target does not resolve to a publicly routable address")
	// ErrRedirected reports a target that sent the request somewhere else.
	ErrRedirected = errors.New("the target redirected the request elsewhere")
	// ErrUnreachable reports a target that could not be reached at all.
	ErrUnreachable = errors.New("the target could not be reached")
)

const (
	// The whole request, including connecting and reading. A verification is a
	// person waiting in front of a screen.
	requestTimeout = 5 * time.Second
	connectTimeout = 2 * time.Second

	// The file holds a token of 43 characters. The bound is what keeps a target
	// that streams for ever from taking the process's memory with it.
	maxProofBytes = 4096

	// http to https on the same host is how a great many sites are configured.
	// Two hops is room for that and for a trailing-slash redirect, and no more.
	maxRedirects = 2

	// A name the owner of a target can recognise in their access log, and
	// nothing else: the request goes to a host somebody else controls.
	userAgent = "Ballast-target-verification/1"
)

// Verifier fetches the challenge a target has to serve.
//
// It reads a URL chosen by whoever declared the environment, from inside
// Ballast's own network, which is the definition of a server-side request
// forgery. Everything below exists because of that: the address guard, the
// redirect policy, the timeouts and the bound on the response.
type Verifier struct {
	client *http.Client
}

// New returns a verifier whose client cannot be made unsafe from outside this
// package: it takes no transport, and the guarded one is the only one it builds.
func New() *Verifier {
	return newWithTransport(guarded(), requestTimeout)
}

// newWithTransport is the seam the tests use. It is unexported on purpose —
// every address a test server can listen on is a loopback address, and the
// guard refuses those, so the happy path has to be exercised without a socket.
func newWithTransport(transport http.RoundTripper, timeout time.Duration) *Verifier {
	return &Verifier{client: &http.Client{
		Transport:     transport,
		Timeout:       timeout,
		CheckRedirect: sameHostOnly,
	}}
}

// guarded builds the transport that will not connect to an address Ballast has
// no business reaching.
func guarded() *http.Transport {
	dialer := &net.Dialer{Timeout: connectTimeout, Control: allowAddress}

	return &http.Transport{
		// No proxy, and in particular not one from the process's environment: a
		// proxy would do the connecting, and the guard below would never see
		// the address that was really reached.
		Proxy:               nil,
		DialContext:         dialer.DialContext,
		TLSHandshakeTimeout: connectTimeout,
		// One verification, one connection. Nothing here is worth keeping open
		// to a host somebody else controls.
		DisableKeepAlives: true,
	}
}

// allowAddress is the guard, and it sits on the dialer rather than in front of
// the request for one reason: the address it is handed is the one the kernel is
// about to connect to, after DNS resolution. Resolving the name separately and
// then letting the client resolve it again leaves a window in which the answer
// can change — a DNS rebinding — and this closes it. It also runs again on
// every redirect hop, since each one dials.
func allowAddress(_, address string, _ syscall.RawConn) error {
	parsed, err := netip.ParseAddrPort(address)
	if err != nil {
		return fmt.Errorf("%w: %s is not an address", ErrAddressRefused, address)
	}
	return publiclyRoutable(parsed.Addr())
}

// publiclyRoutable refuses everything that is not a public address.
func publiclyRoutable(addr netip.Addr) error {
	// An IPv4 address wearing an IPv6 costume, ::ffff:10.0.0.1. With the older
	// net.IP this was the classic hole in a guard like this one; netip's own
	// predicates account for it — checked, not assumed — so unmapping here
	// changes no answer today. It is kept for two smaller reasons: the refusal
	// below then names the address as the v4 one it really is, and a predicate
	// added later that does not account for 4-in-6 cannot reopen the hole.
	addr = addr.Unmap()

	switch {
	case !addr.IsValid():
		return fmt.Errorf("%w: not an address", ErrAddressRefused)

	// IsPrivate covers RFC 1918 for IPv4 and RFC 4193 — fc00::/7 — for IPv6.
	// IsLinkLocalUnicast covers 169.254/16, and with it the cloud metadata
	// service at 169.254.169.254, which is the address this whole guard is
	// most urgently about.
	case addr.IsLoopback(),
		addr.IsPrivate(),
		addr.IsLinkLocalUnicast(),
		addr.IsLinkLocalMulticast(),
		addr.IsInterfaceLocalMulticast(),
		addr.IsMulticast(),
		addr.IsUnspecified():
		return fmt.Errorf("%w: %s", ErrAddressRefused, addr)
	}

	return nil
}

// sameHostOnly refuses a redirect that leaves the host, and stops a chain that
// does not end.
//
// A target may send http to https, or add a trailing slash. It may not send the
// request to another host: the proof is about the host that was declared, and a
// redirect to somewhere else is either a mistake or an attempt to make Ballast
// fetch an address of the attacker's choosing. The address guard would refuse a
// private one on the next dial, and this refuses the rest.
func sameHostOnly(request *http.Request, via []*http.Request) error {
	if len(via) > maxRedirects {
		return fmt.Errorf("%w: more than %d times", ErrRedirected, maxRedirects)
	}

	previous := via[len(via)-1].URL
	if request.URL.Host == "" || !strings.EqualFold(request.URL.Host, previous.Host) {
		return fmt.Errorf("%w: %s sent it to %s", ErrRedirected, previous.Host, request.URL.Host)
	}

	return nil
}

// ReadProof fetches proofURL and reports whether it holds expected.
//
// It returns nil only when the target answered 200 with the token and nothing
// else. Every other outcome is one of this package's sentinels, wrapped with
// something the person who declared the environment can act on.
func (v *Verifier) ReadProof(ctx context.Context, proofURL, expected string) error {
	// A verification nobody is waiting for is not started. Checked here rather
	// than left to the transport, so that it holds whatever the transport is.
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrUnreachable, err)
	}

	parsed, err := url.Parse(proofURL)
	switch {
	case err != nil:
		return fmt.Errorf("%w: the address cannot be read", ErrUnreachable)
	case parsed.Scheme != "http" && parsed.Scheme != "https":
		// Checked here as well as where the base URL is validated: this
		// function is handed a URL and has no business trusting where from.
		return fmt.Errorf("%w: %s is not an http address", ErrUnreachable, parsed.Scheme)
	case parsed.Host == "":
		return fmt.Errorf("%w: the address names no host", ErrUnreachable)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, proofURL, nil)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Accept", "text/plain")

	response, err := v.client.Do(request)
	if err != nil {
		return reachError(err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: the target answered %d", ErrProofMissing, response.StatusCode)
	}

	// One byte past the bound, so that a file which is too long is told apart
	// from one that merely fills it.
	body, err := io.ReadAll(io.LimitReader(response.Body, maxProofBytes+1))
	if err != nil {
		return fmt.Errorf("%w: the file could not be read: %v", ErrUnreachable, err)
	}
	if len(body) > maxProofBytes {
		return fmt.Errorf("%w: the file is larger than %d bytes", ErrProofMismatch, maxProofBytes)
	}

	// Trimmed, because a text editor adds a newline and that is not a reason to
	// refuse a proof. Nothing else is tolerated: the file holds the token and
	// not a page that happens to contain it.
	if strings.TrimSpace(string(body)) != expected {
		return fmt.Errorf("%w", ErrProofMismatch)
	}

	return nil
}

// reachError keeps the reason a request did not happen. The client wraps
// everything in *url.Error, which unwraps, so the sentinels the guard and the
// redirect policy returned are still in there.
func reachError(err error) error {
	switch {
	case errors.Is(err, ErrAddressRefused):
		return fmt.Errorf("%w: %v", ErrAddressRefused, unwrapURL(err))
	case errors.Is(err, ErrRedirected):
		return fmt.Errorf("%w: %v", ErrRedirected, unwrapURL(err))
	default:
		return fmt.Errorf("%w: %v", ErrUnreachable, unwrapURL(err))
	}
}

// unwrapURL drops the *url.Error wrapper, whose message repeats the URL — and
// the URL carries the token.
func unwrapURL(err error) error {
	var wrapped *url.Error
	if errors.As(err, &wrapped) {
		return wrapped.Err
	}
	return err
}

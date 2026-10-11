// Written before the implementation. This is the only path out of the run
// network, so what it refuses is the whole of what a scenario cannot reach.
//
// An internal test package: the tunnel tests need a listener of their own, and
// every address one can listen on is a loopback address, which the real address
// guard refuses by design. The guard is tested exhaustively in infra/netguard;
// here it is a double for everything except the one test that is about it.
package egress

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/youssefmahersi/ballast/internal/infra/netguard"
)

// permissive stands in for the address guard where the test is about the host
// allowlist or the tunnel rather than about addresses.
type permissive struct{}

func (permissive) Allow(netip.Addr) error { return nil }

// fixedResolver answers whatever the test set up, which is how a DNS answer that
// changes under the proxy's feet is reproducible.
type fixedResolver map[string][]netip.Addr

func (f fixedResolver) Resolve(_ context.Context, host string) ([]netip.Addr, error) {
	if found, ok := f[host]; ok {
		return found, nil
	}
	return nil, fmt.Errorf("no such host: %s", host)
}

// target is a plain TCP server that answers a line and echoes the rest, so a
// test can tell a tunnel that carries bytes from one that merely opened.
//
// It returns the address it really listens on. The tests then put that port in
// the allowlist and ask for it, rather than the proxy growing a way to send a
// request somewhere other than where it was addressed.
func target(t *testing.T) netip.AddrPort {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				_, _ = io.WriteString(conn, "hello from the target\n")
				_, _ = io.Copy(conn, conn)
			}()
		}
	}()

	return netip.MustParseAddrPort(listener.Addr().String())
}

// httpTarget answers one HTTP request, and records what it was asked for.
func httpTarget(t *testing.T, seen *string) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	server := &http.Server{
		ReadHeaderTimeout: 2 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*seen = r.Host + r.URL.Path
			_, _ = io.WriteString(w, "served")
		}),
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })

	return listener.Addr().String()
}

// proxy starts a proxy in front of the test and returns its address.
func proxy(t *testing.T, cfg Config) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("build the proxy: %v", err)
	}

	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })

	return listener.Addr().String()
}

// connect speaks the one request a generator makes for an https target, and
// returns the proxy's answer along with the reader that read it.
//
// The reader matters. Reading the response buffers whatever arrived with it, and
// what arrives on a tunnel's heels is the target's first bytes — so a caller
// that opens a second reader on the connection waits for data another buffer has
// already swallowed, for ever. That is not a hypothetical: it hung this suite,
// and on a machine where the test binary's own ten-minute limit was the first
// thing to notice.
func connect(t *testing.T, proxyAddr, hostPort string) (*http.Response, *bufio.Reader) {
	t.Helper()

	conn, err := net.DialTimeout("tcp", proxyAddr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial the proxy: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	// So that a tunnel which never carries what it should fails in seconds with
	// something to read, rather than holding the whole suite until Go's own
	// limit runs out.
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("set a deadline: %v", err)
	}

	if _, err := fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", hostPort, hostPort); err != nil {
		t.Fatalf("write CONNECT: %v", err)
	}

	reader := bufio.NewReader(conn)
	res, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatalf("read the proxy's answer: %v", err)
	}

	return res, reader
}

// ---------------------------------------------------------------------------
// The host allowlist
// ---------------------------------------------------------------------------

func TestATunnelToAnAllowedHostCarriesBytes(t *testing.T) {
	addr := target(t)
	allowed := fmt.Sprintf("staging.example.test:%d", addr.Port())

	proxyAddr := proxy(t, Config{
		Allowed:  []string{allowed},
		Guard:    permissive{},
		Resolver: fixedResolver{"staging.example.test": {addr.Addr()}},
	})

	res, tunnel := connect(t, proxyAddr, allowed)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT answered %d, want 200", res.StatusCode)
	}

	// From the same reader the response came through, not a new one.
	greeting, err := tunnel.ReadString('\n')
	if err != nil {
		t.Fatalf("read through the tunnel: %v", err)
	}
	if strings.TrimSpace(greeting) != "hello from the target" {
		t.Fatalf("the tunnel carried %q", greeting)
	}
}

// The first acceptance criterion of the ticket, at this layer.
func TestATunnelToAHostThatIsNotAllowedIsRefused(t *testing.T) {
	refused := []string{
		"not-ours.example.test:443",
		// Not a suffix match, not a prefix match: the host is the whole string.
		"evil-staging.example.test:443",
		"staging.example.test.evil.test:443",
		"staging.example.test:22",
		"staging.example.test:80",
		"10.0.0.1:443",
		"169.254.169.254:80",
	}

	proxyAddr := proxy(t, Config{
		Allowed:  []string{"staging.example.test:443"},
		Guard:    permissive{},
		Resolver: fixedResolver{},
	})

	for _, hostPort := range refused {
		t.Run(hostPort, func(t *testing.T) {
			res, _ := connect(t, proxyAddr, hostPort)
			if res.StatusCode != http.StatusForbidden {
				t.Fatalf("CONNECT %s answered %d, want 403", hostPort, res.StatusCode)
			}
		})
	}
}

// The port is part of the allowlist, not just the host. An environment declares
// one, and a tunnel to another port of the same machine is not what was approved.
func TestThePortIsPartOfWhatIsAllowed(t *testing.T) {
	addr := target(t)

	proxyAddr := proxy(t, Config{
		Allowed:  []string{fmt.Sprintf("staging.example.test:%d", addr.Port())},
		Guard:    permissive{},
		Resolver: fixedResolver{"staging.example.test": {addr.Addr()}},
	})

	allowed, _ := connect(t, proxyAddr, fmt.Sprintf("staging.example.test:%d", addr.Port()))
	if allowed.StatusCode != http.StatusOK {
		t.Fatalf("the allowed port answered %d", allowed.StatusCode)
	}

	// The same machine, one port along.
	refused, _ := connect(t, proxyAddr, fmt.Sprintf("staging.example.test:%d", addr.Port()+1))
	if refused.StatusCode != http.StatusForbidden {
		t.Fatalf("another port answered %d, want 403", refused.StatusCode)
	}
}

func TestTheHostIsMatchedWithoutRegardToCase(t *testing.T) {
	addr := target(t)

	proxyAddr := proxy(t, Config{
		Allowed:  []string{fmt.Sprintf("staging.example.test:%d", addr.Port())},
		Guard:    permissive{},
		Resolver: fixedResolver{"staging.example.test": {addr.Addr()}},
	})

	res, _ := connect(t, proxyAddr, fmt.Sprintf("STAGING.Example.Test:%d", addr.Port()))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("a host in another case answered %d", res.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// The address, re-resolved by the proxy
// ---------------------------------------------------------------------------

// The second acceptance criterion, and the reason the proxy resolves rather than
// trusting the client: an allowed host whose DNS answer points inside the
// network Ballast runs on. The host is on the list, and the address is not
// something a script may reach.
func TestAnAllowedHostResolvingSomewherePrivateIsRefused(t *testing.T) {
	for name, resolved := range map[string]string{
		"a private address":    "10.0.0.1",
		"the metadata service": "169.254.169.254",
		"the loopback":         "127.0.0.1",
	} {
		t.Run(name, func(t *testing.T) {
			proxyAddr := proxy(t, Config{
				Allowed: []string{"staging.example.test:443"},
				// The real guard, not the double: this test is about it.
				Guard:    netguard.New(),
				Resolver: fixedResolver{"staging.example.test": {netip.MustParseAddr(resolved)}},
			})

			// 403 and not 502: the refusal came from the guard, before any
			// connection was attempted. A proxy that dialed and failed would
			// answer that it could not reach the target.
			res, _ := connect(t, proxyAddr, "staging.example.test:443")
			if res.StatusCode != http.StatusForbidden {
				t.Fatalf("a host resolving to %s answered %d, want 403", resolved, res.StatusCode)
			}
		})
	}
}

// A resolver answering with a public address and a private one is refused
// outright rather than dialed on the public one. Picking the allowed answer out
// of a mixed set would mean the order of a DNS reply decides whether a request
// reaches somebody's internal network.
func TestAMixedAnswerIsRefusedRatherThanFilteredDown(t *testing.T) {
	proxyAddr := proxy(t, Config{
		Allowed: []string{"staging.example.test:443"},
		Guard:   netguard.New(),
		Resolver: fixedResolver{"staging.example.test": {
			netip.MustParseAddr("93.184.216.34"),
			netip.MustParseAddr("10.0.0.1"),
		}},
	})

	res, _ := connect(t, proxyAddr, "staging.example.test:443")
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("a mixed answer gave %d, want 403", res.StatusCode)
	}
}

func TestAHostThatDoesNotResolveIsRefused(t *testing.T) {
	proxyAddr := proxy(t, Config{
		Allowed:  []string{"staging.example.test:443"},
		Guard:    permissive{},
		Resolver: fixedResolver{},
	})

	res, _ := connect(t, proxyAddr, "staging.example.test:443")
	if res.StatusCode == http.StatusOK {
		t.Fatal("a host that resolves to nothing was tunnelled")
	}
}

// ---------------------------------------------------------------------------
// Plain HTTP, which arrives as an absolute URI rather than as CONNECT
// ---------------------------------------------------------------------------

func TestAPlainRequestToAnAllowedHostIsForwarded(t *testing.T) {
	var asked string
	targetAddr := httpTarget(t, &asked)
	addr := netip.MustParseAddrPort(targetAddr)

	proxyAddr := proxy(t, Config{
		Allowed:  []string{fmt.Sprintf("staging.example.test:%d", addr.Port())},
		Guard:    permissive{},
		Resolver: fixedResolver{"staging.example.test": {addr.Addr()}},
	})

	client := &http.Client{
		Transport: &http.Transport{Proxy: func(*http.Request) (*url.URL, error) {
			return url.Parse("http://" + proxyAddr)
		}},
		Timeout: 3 * time.Second,
	}

	res, err := client.Get(fmt.Sprintf("http://staging.example.test:%d/healthz", addr.Port()))
	if err != nil {
		t.Fatalf("through the proxy: %v", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	if string(body) != "served" {
		t.Fatalf("the body is %q", body)
	}
	if !strings.HasPrefix(asked, "staging.example.test") {
		t.Fatalf("the target was asked for %q; the Host header did not survive", asked)
	}
}

func TestAPlainRequestToAHostThatIsNotAllowedIsRefused(t *testing.T) {
	proxyAddr := proxy(t, Config{
		Allowed:  []string{"staging.example.test:80"},
		Guard:    permissive{},
		Resolver: fixedResolver{},
	})

	client := &http.Client{
		Transport: &http.Transport{Proxy: func(*http.Request) (*url.URL, error) {
			return url.Parse("http://" + proxyAddr)
		}},
		Timeout: 3 * time.Second,
	}

	res, err := client.Get("http://not-ours.example.test/")
	if err != nil {
		t.Fatalf("through the proxy: %v", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("status %d, want 403", res.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// What the proxy is not
// ---------------------------------------------------------------------------

// A request in origin form — GET /path — is somebody talking to the proxy as if
// it were a web server. It is not one, and answering as one would make it a way
// to probe whatever it can reach.
func TestTheProxyIsNotAWebServer(t *testing.T) {
	proxyAddr := proxy(t, Config{
		Allowed:  []string{"staging.example.test:443"},
		Guard:    permissive{},
		Resolver: fixedResolver{},
	})

	conn, err := net.DialTimeout("tcp", proxyAddr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	if _, err := io.WriteString(conn, "GET /healthz HTTP/1.1\r\nHost: proxy\r\n\r\n"); err != nil {
		t.Fatalf("write: %v", err)
	}

	res, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodGet})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", res.StatusCode)
	}
}

// An allowlist with nothing in it allows nothing. The failure mode to avoid is
// the one where an empty list reads as "no restriction".
func TestAnEmptyAllowlistAllowsNothing(t *testing.T) {
	proxyAddr := proxy(t, Config{
		Guard:    permissive{},
		Resolver: fixedResolver{},
	})

	res, _ := connect(t, proxyAddr, "staging.example.test:443")
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("an empty allowlist answered %d, want 403", res.StatusCode)
	}
}

func TestAnEntryThatIsNotHostAndPortIsRefusedAtStartup(t *testing.T) {
	for _, bad := range []string{"staging.example.test", "staging.example.test:", ":443", "", "staging.example.test:http"} {
		if _, err := New(Config{Allowed: []string{bad}, Guard: permissive{}}); err == nil {
			t.Errorf("%q was accepted as an allowlist entry", bad)
		}
	}
}

func TestAProxyWithNoGuardIsRefusedAtStartup(t *testing.T) {
	if _, err := New(Config{Allowed: []string{"staging.example.test:443"}}); !errors.Is(err, ErrNoGuard) {
		t.Fatalf("a proxy with no address guard was built: %v", err)
	}
}

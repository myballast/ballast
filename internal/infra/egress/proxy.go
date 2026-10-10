// Package egress is the only way out of a run's network.
//
// A generator runs JavaScript somebody uploaded. The network it sits on has no
// route of its own, so this proxy is the whole of what a script can reach, and
// what it refuses is the whole of what a script cannot.
//
// Two gates, asked in this order. The host: a tunnel is opened only to a
// host:port the run was allowed, matched exactly. The address: the host is
// resolved here rather than by the client, and every address it answers with
// has to be one Ballast will connect to — which is what keeps a name on the
// allowlist from being pointed at the network Ballast itself runs on.
//
// It never looks inside a tunnel. An https target arrives as CONNECT and is
// copied byte for byte, so the credentials a scenario authenticates with stay
// between it and the target.
package egress

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrNoGuard reports a proxy built without an address guard, which would be a
// proxy with one of its two gates missing.
var ErrNoGuard = errors.New("egress: no address guard")

// errRefused marks a decision rather than a failure.
//
// The two must not share a status code. A target that is forbidden and one that
// is merely down are read differently by whoever is looking at a run's log, and
// a test that cannot tell them apart passes while the policy is broken — which
// is exactly what happened to the first version of this file.
var errRefused = errors.New("refused")

const (
	// A target that has not answered by now is not going to, and a generator is
	// waiting on it.
	dialTimeout = 10 * time.Second
	// Long enough for a soak test's connection to stay open.
	idleTimeout = 10 * time.Minute
)

// AddressGuard answers whether an address may be connected to. Declared here,
// by the code that consumes it, and satisfied by infra/netguard.
type AddressGuard interface {
	Allow(addr netip.Addr) error
}

// Resolver turns a host into addresses. Declared so that a DNS answer which
// changes under the proxy's feet is something a test can produce.
type Resolver interface {
	Resolve(ctx context.Context, host string) ([]netip.Addr, error)
}

// Config is what a proxy is built from.
type Config struct {
	// Allowed are the host:port pairs this run may reach. Exactly those: a
	// host with no port, or a port on an allowed host that was not named, is
	// refused. An empty list allows nothing.
	Allowed []string
	// Guard is required.
	Guard AddressGuard
	// Resolver defaults to the system's.
	Resolver Resolver
	Log      *slog.Logger
}

// Proxy serves the one way out.
type Proxy struct {
	allowed  map[string]struct{}
	guard    AddressGuard
	resolver Resolver
	log      *slog.Logger
	server   *http.Server
}

// New builds a proxy, refusing a configuration that would not be one.
func New(cfg Config) (*Proxy, error) {
	if cfg.Guard == nil {
		return nil, ErrNoGuard
	}

	allowed := make(map[string]struct{}, len(cfg.Allowed))
	for _, entry := range cfg.Allowed {
		normalised, err := normalise(entry)
		if err != nil {
			return nil, err
		}
		allowed[normalised] = struct{}{}
	}

	resolver := cfg.Resolver
	if resolver == nil {
		resolver = systemResolver{}
	}
	log := cfg.Log
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	proxy := &Proxy{allowed: allowed, guard: cfg.Guard, resolver: resolver, log: log}
	proxy.server = &http.Server{
		Handler:           proxy,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       idleTimeout,
	}

	return proxy, nil
}

// normalise reads an allowlist entry, which must name a host and a port.
//
// The port is part of what was allowed. An environment declares one, and a
// tunnel to another port of the same machine is not what anybody approved.
func normalise(entry string) (string, error) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(entry))
	if err != nil {
		return "", fmt.Errorf("egress: %q is not a host and a port: %w", entry, err)
	}
	if host == "" {
		return "", fmt.Errorf("egress: %q names no host", entry)
	}

	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return "", fmt.Errorf("egress: %q does not name a port", entry)
	}

	return strings.ToLower(host) + ":" + port, nil
}

// Serve answers on listener until Close.
func (p *Proxy) Serve(listener net.Listener) error {
	if err := p.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Close stops serving.
func (p *Proxy) Close() error {
	return p.server.Close()
}

// ServeHTTP handles the two shapes a request through a proxy takes, and refuses
// everything else.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		p.tunnel(w, r)
		return
	}

	// A request in proxy form names an absolute URL. One in origin form —
	// GET /path — is somebody talking to this as if it were a web server, and
	// answering as one would make it a way to probe whatever it can reach.
	if !r.URL.IsAbs() {
		http.Error(w, "this is a proxy, not a server", http.StatusBadRequest)
		return
	}

	p.forward(w, r)
}

// tunnel handles CONNECT, which is how every https target arrives.
func (p *Proxy) tunnel(w http.ResponseWriter, r *http.Request) {
	target, err := p.dial(r.Context(), r.Host)
	if err != nil {
		p.refuse(w, r.Host, err)
		return
	}
	defer func() { _ = target.Close() }()

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "the tunnel cannot be opened", http.StatusInternalServerError)
		return
	}

	client, buffered, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, "the tunnel cannot be opened", http.StatusInternalServerError)
		return
	}
	defer func() { _ = client.Close() }()

	if _, err := io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}

	// Byte for byte, in both directions, with nothing read along the way. This
	// is what keeps the credentials a scenario authenticates with out of
	// Ballast's hands.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(target, buffered)
		_ = closeWrite(target)
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(client, target)
		_ = closeWrite(client)
	}()
	wg.Wait()
}

// forward handles a plain http request, which arrives with an absolute URL.
func (p *Proxy) forward(w http.ResponseWriter, r *http.Request) {
	hostPort := r.URL.Host
	if r.URL.Port() == "" {
		hostPort = net.JoinHostPort(r.URL.Hostname(), "80")
	}

	conn, err := p.dial(r.Context(), hostPort)
	if err != nil {
		p.refuse(w, hostPort, err)
		return
	}
	defer func() { _ = conn.Close() }()

	outbound := r.Clone(r.Context())
	outbound.RequestURI = ""
	// Hop-by-hop, and not ours to pass on.
	outbound.Header.Del("Proxy-Connection")
	outbound.Header.Del("Proxy-Authorization")

	if err := outbound.Write(conn); err != nil {
		http.Error(w, "the target could not be written to", http.StatusBadGateway)
		return
	}

	response, err := http.ReadResponse(bufio.NewReader(conn), outbound)
	if err != nil {
		http.Error(w, "the target did not answer", http.StatusBadGateway)
		return
	}
	defer func() { _ = response.Body.Close() }()

	for name, values := range response.Header {
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
}

// dial is where both gates are, and in this order: a host nobody allowed is
// refused without being resolved, so the proxy is not a way to find out what
// resolves.
func (p *Proxy) dial(ctx context.Context, hostPort string) (net.Conn, error) {
	asked, err := normalise(hostPort)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errRefused, err)
	}
	if _, ok := p.allowed[asked]; !ok {
		return nil, fmt.Errorf("%w: %s is not a host this run may reach", errRefused, asked)
	}

	host, port, _ := net.SplitHostPort(asked)

	// Resolved here rather than by the client. The client asked for a name; what
	// that name means is this proxy's to decide, and deciding it once is what
	// closes the gap a changing DNS answer would otherwise open.
	addrs, err := p.resolver.Resolve(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("%s could not be resolved: %w", host, err)
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("%w: %s resolves to nothing", errRefused, host)
	}

	// Every answer, not the first acceptable one. Filtering a mixed set down
	// would mean the order of a DNS reply decides whether a request reaches
	// somebody's internal network.
	for _, addr := range addrs {
		if err := p.guard.Allow(addr); err != nil {
			return nil, fmt.Errorf("%w: %s resolves to %s, which is not reachable: %v",
				errRefused, host, addr, err)
		}
	}

	// In order, until one answers: a host may resolve to both families while the
	// proxy's own network carries only one of them.
	dialer := net.Dialer{Timeout: dialTimeout}
	var dialErr error
	for _, addr := range addrs {
		conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(addr.String(), port))
		if err == nil {
			return conn, nil
		}
		if dialErr == nil {
			dialErr = err
		}
	}

	return nil, fmt.Errorf("%s could not be reached: %w", asked, dialErr)
}

// refuse answers, and says so in the log, which is the record of what a run
// tried to reach.
//
// 403 is a decision of ours; 502 is a target that would have been allowed and
// did not answer. Collapsing the two into one code hides a broken policy behind
// a flaky network, and makes a test of the policy unable to tell that it broke.
func (p *Proxy) refuse(w http.ResponseWriter, hostPort string, reason error) {
	status := http.StatusBadGateway
	if errors.Is(reason, errRefused) {
		status = http.StatusForbidden
	}

	p.log.Warn("egress refused", "host", hostPort, "status", status, "reason", reason.Error())
	http.Error(w, reason.Error(), status)
}

// systemResolver is the real one.
type systemResolver struct{}

func (systemResolver) Resolve(ctx context.Context, host string) ([]netip.Addr, error) {
	// An address literal is already an answer, and resolving it would be asking
	// the system to parse it.
	if addr, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{addr}, nil
	}

	// One family at a time, rather than asking for "ip" and getting both.
	//
	// Docker's embedded resolver answers SERVFAIL to an AAAA query for a
	// container name on a network with no IPv6, and Go turns a failure of
	// either half into "server misbehaving" for the whole lookup. Asked for
	// both at once, a target that resolves perfectly well over IPv4 is simply
	// unreachable — which is how this was found, by a container test rather
	// than by reading.
	var (
		addrs    []netip.Addr
		firstErr error
	)
	for _, family := range []string{"ip4", "ip6"} {
		found, err := net.DefaultResolver.LookupNetIP(ctx, family, host)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		addrs = append(addrs, found...)
	}

	if len(addrs) == 0 {
		if firstErr != nil {
			return nil, firstErr
		}
		return nil, fmt.Errorf("%s resolves to no address", host)
	}

	return addrs, nil
}

// closeWrite ends one direction of a tunnel so the other side sees the end of
// the stream rather than waiting for a timeout.
func closeWrite(conn net.Conn) error {
	if tcp, ok := conn.(*net.TCPConn); ok {
		return tcp.CloseWrite()
	}
	return nil
}

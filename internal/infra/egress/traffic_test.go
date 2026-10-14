package egress

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"testing"
	"time"
)

// What this proxy counts is what crossed the socket to a target, which is the
// figure nothing else in Ballast can see: the generator knows what its script
// sent, only this knows what left the machine.

// counting starts a proxy and hands back its address along with the proxy
// itself, so a test can read what it measured.
func counting(t *testing.T, cfg Config) (string, *Proxy) {
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

	return listener.Addr().String(), server
}

// moved waits for the proxy's totals to settle on what the test expects, and
// reports what it saw.
//
// The copying runs in goroutines of its own, so a byte written here is counted
// a moment later. Waiting for the expected figure rather than reading once is
// what keeps this from failing on a loaded machine; a wrong figure still fails,
// a tenth of a second later.
func moved(t *testing.T, p *Proxy, wantSent, wantReceived int64) (int64, int64) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for {
		sent, received := p.Traffic()
		if sent == wantSent && received == wantReceived {
			return sent, received
		}
		if time.Now().After(deadline) {
			return sent, received
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// tunnelTo opens a tunnel and returns both ends of it: the connection to write
// into, and the reader the proxy's answer came through.
//
// The reader is the one that read the CONNECT response, never a new one: what
// arrives on a tunnel's heels is the target's first bytes, and a second reader
// would wait for ever on data the first already swallowed.
func tunnelTo(t *testing.T, proxyAddr, hostPort string) (net.Conn, *bufio.Reader) {
	t.Helper()

	conn, err := net.DialTimeout("tcp", proxyAddr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial the proxy: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

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
	if res.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT answered %d", res.StatusCode)
	}

	return conn, reader
}

// A tunnel is copied byte for byte and counted the same way. The payload here
// is plain text, which is what makes the arithmetic checkable; in production it
// is TLS, and the proxy counts the records without being able to read them —
// which is exactly why its figure is larger than the generator's.
func TestATunnelCountsEveryByteItCarried(t *testing.T) {
	const question = "a question from the generator\n"
	const greeting = "hello from the target\n"

	addr := target(t)
	allowed := fmt.Sprintf("staging.example.test:%d", addr.Port())

	proxyAddr, proxy := counting(t, Config{
		Allowed:  []string{allowed},
		Guard:    permissive{},
		Resolver: fixedResolver{"staging.example.test": {addr.Addr()}},
	})

	conn, tunnel := tunnelTo(t, proxyAddr, allowed)

	// The target greets, then echoes. Reading both means every byte of this
	// exchange has crossed the proxy.
	if _, err := io.WriteString(conn, question); err != nil {
		t.Fatalf("write through the tunnel: %v", err)
	}
	if _, err := tunnel.ReadString('\n'); err != nil {
		t.Fatalf("read the greeting: %v", err)
	}
	echoed, err := tunnel.ReadString('\n')
	if err != nil {
		t.Fatalf("read the echo: %v", err)
	}
	if echoed != question {
		t.Fatalf("the tunnel echoed %q", echoed)
	}

	wantSent := int64(len(question))
	wantReceived := int64(len(greeting) + len(question))

	sent, received := moved(t, proxy, wantSent, wantReceived)
	if sent != wantSent {
		t.Errorf("the proxy counted %d bytes sent, the generator wrote %d", sent, wantSent)
	}
	if received != wantReceived {
		t.Errorf("the proxy counted %d bytes received, the target wrote %d", received, wantReceived)
	}
}

// A request this proxy refuses never reaches a socket, so it moved nothing.
// A total that grew on refusals would charge somebody for being stopped.
func TestARefusedTunnelCountsNothing(t *testing.T) {
	addr := target(t)

	proxyAddr, proxy := counting(t, Config{
		Allowed:  []string{fmt.Sprintf("staging.example.test:%d", addr.Port())},
		Guard:    permissive{},
		Resolver: fixedResolver{"staging.example.test": {addr.Addr()}},
	})

	res, _ := connect(t, proxyAddr, "not-ours.example.test:443")
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("CONNECT to a host nobody allowed answered %d", res.StatusCode)
	}

	if sent, received := proxy.Traffic(); sent != 0 || received != 0 {
		t.Fatalf("a refused request counted %d sent and %d received", sent, received)
	}
}

// The plain-http path counts the request and the response as they cross the
// socket — headers included. That is the difference this measurement exists
// for: a generator reporting six bytes of body has sent a few hundred.
func TestAPlainRequestCountsItsHeadersToo(t *testing.T) {
	var asked string
	addr := netip.MustParseAddrPort(httpTarget(t, &asked))

	proxyAddr, proxy := counting(t, Config{
		Allowed:  []string{fmt.Sprintf("shop.example.test:%d", addr.Port())},
		Guard:    permissive{},
		Resolver: fixedResolver{"shop.example.test": {addr.Addr()}},
	})

	client := &http.Client{
		Transport: &http.Transport{Proxy: func(*http.Request) (*url.URL, error) {
			return url.Parse("http://" + proxyAddr)
		}},
		Timeout: 3 * time.Second,
	}

	res, err := client.Get(fmt.Sprintf("http://shop.example.test:%d/orders", addr.Port()))
	if err != nil {
		t.Fatalf("through the proxy: %v", err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()

	if string(body) != "served" {
		t.Fatalf("the target answered %q", body)
	}

	sent, received := proxy.Traffic()
	switch {
	case sent <= int64(len("GET /orders HTTP/1.1")):
		t.Errorf("%d bytes were counted as sent, which cannot include the headers", sent)
	case received <= int64(len("served")):
		t.Errorf("%d bytes were counted as received, which cannot include the headers", received)
	}
}

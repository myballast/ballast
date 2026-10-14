// Command egress-proxy is the only way out of a run's network.
//
// It runs in a container of its own, attached both to the run's network — which
// has no route of its own — and to a network that reaches the internet. The
// generator is attached to the first alone, so everything it sends either comes
// through here or goes nowhere.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/youssefmahersi/ballast/internal/infra/egress"
	"github.com/youssefmahersi/ballast/internal/infra/logging"
	"github.com/youssefmahersi/ballast/internal/infra/netguard"
)

func main() {
	log := logging.New("egress-proxy")

	if err := serve(); err != nil {
		log.Error("the proxy stopped", "error", err)
		os.Exit(1)
	}
}

func serve() error {
	log := logging.New("egress-proxy")

	// No default. A proxy started with no allowlist would refuse every request,
	// which looks exactly like a target being down: a wiring mistake has to fail
	// here, loudly, rather than as a run that mysteriously reaches nothing.
	allowed := split(os.Getenv("BALLAST_EGRESS_ALLOWED"))
	if len(allowed) == 0 {
		return errors.New("BALLAST_EGRESS_ALLOWED is empty; it names the host:port pairs this run may reach")
	}

	// Empty in production. It names the networks that are permitted although
	// they are not publicly routable, which is how a run launched on a
	// developer's machine reaches a target on that machine.
	prefixes, err := netguard.ParsePrefixes(split(os.Getenv("BALLAST_EGRESS_ALLOWED_CIDRS")))
	if err != nil {
		return err
	}

	proxy, err := egress.New(egress.Config{
		Allowed: allowed,
		Guard:   netguard.New(prefixes...),
		Log:     log,
	})
	if err != nil {
		return err
	}

	addr := os.Getenv("BALLAST_EGRESS_LISTEN")
	if addr == "" {
		addr = ":3128"
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}

	log.Info("serving the run's only way out",
		"addr", addr, "allowed", allowed, "allowed_networks", len(prefixes))

	// Stopping is how the totals are asked for: the worker stops this container
	// before removing it, and the last line written is the one it reads.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	served := make(chan error, 1)
	go func() { served <- proxy.Serve(listener) }()

	// And a total every few seconds, as a net under that: a proxy killed
	// outright writes no last line, and a run's figure is then short by at most
	// one interval rather than by all of it.
	ticker := time.NewTicker(trafficInterval)
	defer ticker.Stop()

	for {
		select {
		case err := <-served:
			report(log, proxy)
			return err

		case <-ticker.C:
			report(log, proxy)

		case <-ctx.Done():
			report(log, proxy)
			return proxy.Close()
		}
	}
}

// trafficInterval is how often the totals are written while the run is under
// way. Often enough that a proxy killed without warning loses little, rarely
// enough that a soak test does not fill its log with arithmetic.
const trafficInterval = 5 * time.Second

// report writes what the proxy has moved so far, in the one line the worker
// knows how to read.
func report(log *slog.Logger, proxy *egress.Proxy) {
	sent, received := proxy.Traffic()

	log.Info(egress.TrafficMessage, egress.SentField, sent, egress.ReceivedField, received)
}

func split(raw string) []string {
	var entries []string
	for _, entry := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(entry); trimmed != "" {
			entries = append(entries, trimmed)
		}
	}
	return entries
}

// Command egress-proxy is the only way out of a run's network.
//
// It runs in a container of its own, attached both to the run's network — which
// has no route of its own — and to a network that reaches the internet. The
// generator is attached to the first alone, so everything it sends either comes
// through here or goes nowhere.
package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"

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

	return proxy.Serve(listener)
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

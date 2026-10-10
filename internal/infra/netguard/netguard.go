// Package netguard decides which addresses Ballast's own processes may connect
// to on somebody else's behalf.
//
// Two things ask: the target verifier, before it fetches a challenge from an
// address whoever declared the environment chose, and the egress proxy, before
// it dials for a generator running a user's script. Both are reading an address
// that came from outside, from inside Ballast's network, so both need the same
// answer — and one copy of it, because two would drift.
package netguard

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

// ErrNotAllowed reports an address nothing of ours will connect to.
var ErrNotAllowed = errors.New("the address is not one Ballast will connect to")

// Guard answers for a set of addresses.
//
// The zero value is the strict guard, which is what every production path uses:
// public addresses and nothing else.
type Guard struct {
	// allowed are networks named explicitly, which are permitted even though
	// they are not publicly routable. Empty in production. It exists so that a
	// run launched on a developer's machine can reach a target on that machine,
	// and so that a test can reach one on the run's own Docker network.
	allowed []netip.Prefix
}

// New returns a guard that allows public addresses, plus anything inside the
// named networks.
//
// Called with nothing, which is how the verifier calls it, the guard cannot be
// given an exception at all: there is no setter.
func New(allowed ...netip.Prefix) Guard {
	return Guard{allowed: allowed}
}

// Allow reports whether addr may be connected to.
func (g Guard) Allow(addr netip.Addr) error {
	// An IPv4 address wearing an IPv6 costume, ::ffff:10.0.0.1. netip's own
	// predicates account for it — verified rather than assumed — but
	// Prefix.Contains does not consider a v6-mapped v4 address to be inside a
	// v4 prefix, so the named networks below would miss it. Unmapping once here
	// is what makes the two halves agree.
	addr = addr.Unmap()

	if !addr.IsValid() {
		return fmt.Errorf("%w: it is not an address", ErrNotAllowed)
	}

	// Refused whatever anybody names. There is no configuration in which
	// reaching these is what somebody meant: the metadata service hands out the
	// credentials of the machine Ballast runs on, and the loopback is Ballast
	// itself.
	if neverAllowed(addr) {
		return fmt.Errorf("%w: %s is never reachable", ErrNotAllowed, addr)
	}

	for _, prefix := range g.allowed {
		if prefix.Contains(addr) {
			return nil
		}
	}

	// RFC 1918 for IPv4 and RFC 4193 — fc00::/7 — for IPv6. Private unless
	// somebody named the network above, which is the whole of the exception.
	if addr.IsPrivate() {
		return fmt.Errorf("%w: %s is a private address", ErrNotAllowed, addr)
	}

	return nil
}

// neverAllowed is the set no prefix reopens.
//
// IsLinkLocalUnicast covers 169.254.0.0/16, and with it 169.254.169.254, which
// is the address this package most urgently exists for.
func neverAllowed(addr netip.Addr) bool {
	return addr.IsLoopback() ||
		addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() ||
		addr.IsInterfaceLocalMulticast() ||
		addr.IsMulticast() ||
		addr.IsUnspecified()
}

// ParsePrefixes reads networks from configuration.
//
// Anything unreadable stops the caller rather than being skipped: a guard
// holding half the list it was given is one nobody can reason about.
func ParsePrefixes(raw []string) ([]netip.Prefix, error) {
	prefixes := make([]netip.Prefix, 0, len(raw))

	for _, entry := range raw {
		trimmed := strings.TrimSpace(entry)
		if trimmed == "" {
			return nil, errors.New("netguard: an empty network was given")
		}

		prefix, err := netip.ParsePrefix(trimmed)
		if err != nil {
			return nil, fmt.Errorf("netguard: %q is not a network in CIDR form: %w", trimmed, err)
		}
		prefixes = append(prefixes, prefix)
	}

	return prefixes, nil
}

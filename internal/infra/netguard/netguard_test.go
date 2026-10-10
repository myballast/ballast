// The address table moves here intact from adapter/verifier, where it was
// written for the ticket before this one: two things now ask the same question —
// the target verifier before it fetches a challenge, and the egress proxy before
// it dials on a script's behalf — and two copies of a security predicate drift.
package netguard

import (
	"errors"
	"net/netip"
	"testing"
)

func addr(t *testing.T, raw string) netip.Addr {
	t.Helper()

	parsed, err := netip.ParseAddr(raw)
	if err != nil {
		t.Fatalf("the test's own address is wrong: %v", err)
	}
	return parsed
}

func TestAnAddressThatIsNotPubliclyRoutableIsRefused(t *testing.T) {
	t.Parallel()

	refused := map[string]string{
		"the loopback":            "127.0.0.1",
		"anywhere in 127/8":       "127.9.9.9",
		"the IPv6 loopback":       "::1",
		"private 10/8":            "10.0.0.1",
		"private 172.16/12":       "172.16.5.4",
		"private 192.168/16":      "192.168.1.1",
		"link-local":              "169.254.1.1",
		"the cloud metadata host": "169.254.169.254",
		"IPv6 link-local":         "fe80::1",
		"IPv6 unique local":       "fc00::1",
		"the unspecified address": "0.0.0.0",
		"IPv6 unspecified":        "::",
		"multicast":               "224.0.0.1",
		"IPv6 multicast":          "ff02::1",
		// A v4 address wearing a v6 costume, which is the shape that used to
		// slip past a guard like this one.
		"the loopback mapped into IPv6":       "::ffff:127.0.0.1",
		"the metadata host mapped into IPv6":  "::ffff:169.254.169.254",
		"private 10/8 mapped into IPv6":       "::ffff:10.0.0.1",
		"private 192.168/16 mapped into IPv6": "::ffff:192.168.1.1",
	}

	strict := New()
	for name, raw := range refused {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := strict.Allow(addr(t, raw)); !errors.Is(err, ErrNotAllowed) {
				t.Fatalf("%s (%s) was allowed: %v", name, raw, err)
			}
		})
	}
}

// Without this, a guard that refuses everything would pass every test above and
// nothing would ever be reachable.
func TestAPublicAddressIsAllowed(t *testing.T) {
	t.Parallel()

	strict := New()
	for _, raw := range []string{"8.8.8.8", "93.184.216.34", "1.1.1.1", "2606:4700::1111"} {
		if err := strict.Allow(addr(t, raw)); err != nil {
			t.Fatalf("%s is a public address and was refused: %v", raw, err)
		}
	}
}

// A named network is how a development run reaches a target on the machine it is
// launched from, and how a test reaches one on the run's own Docker network.
// Empty in production, which is the only configuration that matters.
func TestANamedNetworkIsAllowed(t *testing.T) {
	t.Parallel()

	guard := New(netip.MustParsePrefix("172.18.0.0/16"))

	if err := guard.Allow(addr(t, "172.18.0.5")); err != nil {
		t.Fatalf("an address inside the named network was refused: %v", err)
	}
	// One prefix does not open its neighbours.
	if err := guard.Allow(addr(t, "172.19.0.5")); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("an address outside the named network was allowed: %v", err)
	}
	// And a public address is still a public address.
	if err := guard.Allow(addr(t, "8.8.8.8")); err != nil {
		t.Fatalf("a public address was refused: %v", err)
	}
}

// Some addresses are refused whatever anybody names, because there is no
// configuration in which reaching them is what somebody meant. The metadata
// service is the one this whole guard exists for: a prefix naming 169.254.0.0/16
// is a mistake or an attack, never a target.
func TestSomeAddressesCannotBeNamedBackIn(t *testing.T) {
	t.Parallel()

	cases := map[string]struct{ prefix, addr string }{
		"the loopback":         {"127.0.0.0/8", "127.0.0.1"},
		"the metadata service": {"169.254.0.0/16", "169.254.169.254"},
		"all of link-local":    {"169.254.0.0/16", "169.254.1.1"},
		"the unspecified one":  {"0.0.0.0/0", "0.0.0.0"},
		"multicast":            {"224.0.0.0/4", "224.0.0.1"},
		"the IPv6 loopback":    {"::/0", "::1"},
		"IPv6 link-local":      {"fe80::/10", "fe80::1"},
		// The widest prefix anybody could write still does not open them.
		"everything at once v4": {"0.0.0.0/0", "127.0.0.1"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			guard := New(netip.MustParsePrefix(tc.prefix))
			if err := guard.Allow(addr(t, tc.addr)); !errors.Is(err, ErrNotAllowed) {
				t.Fatalf("%s was opened by naming %s: %v", name, tc.prefix, err)
			}
		})
	}
}

// A prefix of 0.0.0.0/0 does not turn the guard off for ordinary private ranges
// either — it is a prefix like any other, and private addresses inside it are
// allowed because that is what naming it means. Worth pinning so that nobody
// reads the test above as "wide prefixes are ignored".
func TestAWidePrefixDoesAllowWhatItCovers(t *testing.T) {
	t.Parallel()

	guard := New(netip.MustParsePrefix("0.0.0.0/0"))
	if err := guard.Allow(addr(t, "10.0.0.1")); err != nil {
		t.Fatalf("naming the whole of IPv4 did not allow a private address: %v", err)
	}
}

func TestAnInvalidAddressIsRefused(t *testing.T) {
	t.Parallel()

	if err := New().Allow(netip.Addr{}); !errors.Is(err, ErrNotAllowed) {
		t.Fatal("the zero address was allowed")
	}
}

// The prefixes arrive as text, from configuration. Anything unreadable has to
// stop the process rather than be skipped: a guard configured with half its
// list is a guard nobody can reason about.
func TestReadingPrefixesFromConfiguration(t *testing.T) {
	t.Parallel()

	read, err := ParsePrefixes([]string{"172.18.0.0/16", " 10.0.0.0/8 "})
	if err != nil {
		t.Fatalf("ParsePrefixes: %v", err)
	}
	if len(read) != 2 {
		t.Fatalf("read %d prefixes", len(read))
	}

	for _, bad := range []string{"not a prefix", "172.18.0.0", "172.18.0.0/33", ""} {
		if _, err := ParsePrefixes([]string{bad}); err == nil {
			t.Errorf("%q was accepted as a prefix", bad)
		}
	}

	empty, err := ParsePrefixes(nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("no prefixes gave %v, %v", empty, err)
	}
}

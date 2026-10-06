package domain

import (
	"errors"
	"testing"
	"time"
)

func windowed(t *testing.T, start, end int, zone string) Environment {
	t.Helper()

	return Environment{
		Window: &AllowedWindow{StartMinute: start, EndMinute: end, Zone: zone},
	}
}

func at(t *testing.T, clock string) time.Time {
	t.Helper()

	parsed, err := time.Parse(time.RFC3339, clock)
	if err != nil {
		t.Fatalf("the test's own timestamp is wrong: %v", err)
	}
	return parsed
}

func TestAnEnvironmentWithNoWindowTakesLoadAtAnyHour(t *testing.T) {
	t.Parallel()

	var anytime Environment
	if !anytime.AllowsAt(at(t, "2026-10-06T03:17:00Z")) {
		t.Fatal("an environment that declared no window refused a run")
	}
}

func TestAWindowWithinOneDay(t *testing.T) {
	t.Parallel()

	// 20:00 to 23:00 UTC.
	env := windowed(t, 20*60, 23*60, "UTC")

	cases := map[string]struct {
		clock   string
		allowed bool
	}{
		"before it opens":     {"2026-10-06T19:59:00Z", false},
		"the minute it opens": {"2026-10-06T20:00:00Z", true},
		"in the middle":       {"2026-10-06T21:30:00Z", true},
		"the minute it shuts": {"2026-10-06T23:00:00Z", false},
		"after it shuts":      {"2026-10-06T23:01:00Z", false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := env.AllowsAt(at(t, tc.clock)); got != tc.allowed {
				t.Fatalf("%s: allowed=%v, wanted %v", tc.clock, got, tc.allowed)
			}
		})
	}
}

// The window people actually ask for: load at night, when nobody is using the
// staging environment. Its start is after its end, and the naive comparison
// refuses every hour of the day.
func TestAWindowThatCrossesMidnight(t *testing.T) {
	t.Parallel()

	// 22:00 to 06:00 UTC.
	env := windowed(t, 22*60, 6*60, "UTC")

	cases := map[string]struct {
		clock   string
		allowed bool
	}{
		"the evening it opens": {"2026-10-06T22:30:00Z", true},
		"just before midnight": {"2026-10-06T23:59:00Z", true},
		"after midnight":       {"2026-10-07T00:30:00Z", true},
		"the minute it shuts":  {"2026-10-07T06:00:00Z", false},
		"the working day":      {"2026-10-07T14:00:00Z", false},
		"just before it opens": {"2026-10-07T21:59:00Z", false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := env.AllowsAt(at(t, tc.clock)); got != tc.allowed {
				t.Fatalf("%s: allowed=%v, wanted %v", tc.clock, got, tc.allowed)
			}
		})
	}
}

// The window is wall-clock time where the people who own the target are, which
// is the only reading that makes "not during office hours" mean anything.
func TestTheWindowIsReadInItsOwnZone(t *testing.T) {
	t.Parallel()

	// 22:00 to 23:00 in Paris, which in October is 20:00 to 21:00 UTC.
	env := windowed(t, 22*60, 23*60, "Europe/Paris")

	if !env.AllowsAt(at(t, "2026-10-06T20:30:00Z")) {
		t.Fatal("22:30 in Paris was refused by a window that is open from 22:00 to 23:00 there")
	}
	if env.AllowsAt(at(t, "2026-10-06T22:30:00Z")) {
		t.Fatal("the window was read in UTC rather than in its own zone")
	}
}

// Daylight saving moves the window against UTC, and the window keeps meaning
// the same hour locally. Comparing in UTC instead would silently shift it twice
// a year.
func TestTheWindowFollowsDaylightSaving(t *testing.T) {
	t.Parallel()

	env := windowed(t, 22*60, 23*60, "Europe/Paris")

	// Same local hour, July: Paris is two hours ahead of UTC.
	if !env.AllowsAt(at(t, "2026-07-06T20:30:00Z")) {
		t.Fatal("22:30 in Paris in July was refused")
	}
	if env.AllowsAt(at(t, "2026-07-06T21:30:00Z")) {
		t.Fatal("the window did not follow summer time")
	}
}

// A zone the machine cannot resolve means the rule cannot be evaluated. The
// answer to that is no run, not every run.
func TestAZoneThatCannotBeLoadedRefusesTheRun(t *testing.T) {
	t.Parallel()

	env := windowed(t, 0, 60, "Mars/Olympus_Mons")
	if env.AllowsAt(at(t, "2026-10-06T00:30:00Z")) {
		t.Fatal("an unresolvable zone let a run through; it must fail closed")
	}
}

func TestAWindowBallastWillNotStore(t *testing.T) {
	t.Parallel()

	cases := map[string]AllowedWindow{
		"a negative start":    {StartMinute: -1, EndMinute: 60, Zone: "UTC"},
		"an end past the day": {StartMinute: 0, EndMinute: 1440, Zone: "UTC"},
		"no zone":             {StartMinute: 0, EndMinute: 60, Zone: ""},
		"an unknown zone":     {StartMinute: 0, EndMinute: 60, Zone: "Nowhere/At_All"},
		"a window of nothing": {StartMinute: 540, EndMinute: 540, Zone: "UTC"},
	}

	for name, window := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := window.Validate(); !errors.Is(err, ErrInvalidEnvironment) {
				t.Fatalf("%s was accepted: %v", name, err)
			}
		})
	}

	ok := AllowedWindow{StartMinute: 22 * 60, EndMinute: 6 * 60, Zone: "Europe/Paris"}
	if err := ok.Validate(); err != nil {
		t.Fatalf("a night window in Paris was refused: %v", err)
	}
}

func TestABaseURLBallastWillNotStore(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"empty":               "",
		"no scheme":           "staging.example.test",
		"not http":            "ftp://staging.example.test",
		"a file":              "file:///etc/passwd",
		"no host":             "http://",
		"credentials in it":   "http://user:pass@staging.example.test",
		"a query string":      "https://staging.example.test/?token=abc",
		"a fragment":          "https://staging.example.test/#part",
		"not a url at all":    "http://a b c",
		"a control character": "https://staging.example.test/\n",
	}

	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := ValidateEnvironment("staging", raw, 10, 60); !errors.Is(err, ErrInvalidEnvironment) {
				t.Fatalf("%s (%q) was accepted as a base URL: %v", name, raw, err)
			}
		})
	}

	for _, raw := range []string{
		"https://staging.example.test",
		"http://staging.example.test:8080",
		"https://staging.example.test/api/v2",
	} {
		if err := ValidateEnvironment("staging", raw, 10, 60); err != nil {
			t.Fatalf("%q should be a usable base URL: %v", raw, err)
		}
	}
}

func TestCeilingsBallastWillNotStore(t *testing.T) {
	t.Parallel()

	const url = "https://staging.example.test"

	cases := map[string]struct{ vus, duration int }{
		"no virtual users":    {0, 60},
		"negative users":      {-1, 60},
		"no duration":         {10, 0},
		"negative duration":   {10, -60},
		"more users than any": {100_001, 60},
		"longer than a day":   {10, 86_401},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := ValidateEnvironment("staging", url, tc.vus, tc.duration); !errors.Is(err, ErrInvalidEnvironment) {
				t.Fatalf("%s was accepted: %v", name, err)
			}
		})
	}
}

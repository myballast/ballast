package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// Written before the implementation. A window is a rule about hours; days of
// the week narrow it to some of them. What these tests pin down is which day an
// hour belongs to, since the rule that matters is the one for a window that
// crosses midnight: "mon–fri 22:00–04:00" is five nights, each of which ends the
// morning after it started.

func weekdays(t *testing.T, start, end int, zone string, days ...time.Weekday) Environment {
	t.Helper()

	return Environment{Window: &AllowedWindow{
		StartMinute: start, EndMinute: end, Zone: zone, Days: DaysOf(days...),
	}}
}

// 2026-10-05 is a Monday.
func TestAWindowWithNoDaysIsEveryDay(t *testing.T) {
	t.Parallel()

	env := windowed(t, 22*60, 4*60, "UTC")
	for day := 5; day <= 11; day++ {
		clock := time.Date(2026, 10, day, 23, 0, 0, 0, time.UTC)
		if !allowsAt(env, clock) {
			t.Errorf("a window that names no days refused %s", clock.Weekday())
		}
	}
}

func TestAWindowOnSomeDaysRefusesTheOthers(t *testing.T) {
	t.Parallel()

	// Office hours, Monday to Friday.
	env := weekdays(t, 9*60, 17*60, "UTC",
		time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday)

	cases := map[string]struct {
		clock   string
		allowed bool
	}{
		"monday at noon":          {"2026-10-05T12:00:00Z", true},
		"friday at noon":          {"2026-10-09T12:00:00Z", true},
		"saturday at noon":        {"2026-10-10T12:00:00Z", false},
		"sunday at noon":          {"2026-10-11T12:00:00Z", false},
		"monday before it opens":  {"2026-10-05T08:59:00Z", false},
		"saturday is not made up": {"2026-10-10T09:00:00Z", false},
	}
	for name, c := range cases {
		if got := allowsAt(env, at(t, c.clock)); got != c.allowed {
			t.Errorf("%s: allowed=%v, want %v", name, got, c.allowed)
		}
	}
}

// The night belongs to the day it starts. Friday night runs until Saturday
// morning, and Sunday night's morning is Monday's: a Monday morning is only
// inside the window if Sunday is one of the days.
func TestAWindowThatCrossesMidnightBelongsToTheDayItStarts(t *testing.T) {
	t.Parallel()

	env := weekdays(t, 22*60, 4*60, "UTC",
		time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday)

	cases := map[string]struct {
		clock   string
		allowed bool
	}{
		"friday 23:00, the night begins":      {"2026-10-09T23:00:00Z", true},
		"saturday 02:00, the night of friday": {"2026-10-10T02:00:00Z", true},
		"saturday 04:00, it has closed":       {"2026-10-10T04:00:00Z", false},
		"saturday 23:00, no night begins":     {"2026-10-10T23:00:00Z", false},
		"sunday 02:00, saturday has no night": {"2026-10-11T02:00:00Z", false},
		"sunday 23:00, no night begins":       {"2026-10-11T23:00:00Z", false},
		"monday 02:00, sunday has no night":   {"2026-10-12T02:00:00Z", false},
		"monday 23:00, the night begins":      {"2026-10-12T23:00:00Z", true},
		"tuesday 03:59, the night of monday":  {"2026-10-13T03:59:00Z", true},
		"friday 03:00, the night of thursday": {"2026-10-09T03:00:00Z", true},
		"friday 12:00, between two nights":    {"2026-10-09T12:00:00Z", false},
		"saturday 21:59, the evening before":  {"2026-10-10T21:59:00Z", false},
	}
	for name, c := range cases {
		if got := allowsAt(env, at(t, c.clock)); got != c.allowed {
			t.Errorf("%s: allowed=%v, want %v", name, got, c.allowed)
		}
	}
}

func TestAWeekendOnlyWindow(t *testing.T) {
	t.Parallel()

	env := weekdays(t, 0, 23*60+59, "UTC", time.Saturday, time.Sunday)

	if !allowsAt(env, at(t, "2026-10-10T10:00:00Z")) || !allowsAt(env, at(t, "2026-10-11T10:00:00Z")) {
		t.Error("the weekend was refused")
	}
	if allowsAt(env, at(t, "2026-10-12T10:00:00Z")) || allowsAt(env, at(t, "2026-10-09T10:00:00Z")) {
		t.Error("a weekday was accepted by a weekend window")
	}
}

// The day is the day in the window's zone. At 23:30 UTC on a Friday it is
// already Saturday in Paris, and a Friday-only window there is shut.
func TestTheDayIsReadInTheWindowsOwnZone(t *testing.T) {
	t.Parallel()

	paris := weekdays(t, 0, 23*60+59, "Europe/Paris", time.Friday)

	if !allowsAt(paris, at(t, "2026-10-09T12:00:00Z")) {
		t.Error("friday noon in Paris was refused")
	}
	if allowsAt(paris, at(t, "2026-10-09T23:30:00Z")) {
		t.Error("23:30 UTC on a Friday is Saturday in Paris, and was accepted")
	}
	if !allowsAt(paris, at(t, "2026-10-08T22:30:00Z")) {
		t.Error("22:30 UTC on a Thursday is Friday in Paris, and was refused")
	}
}

// How long is left is measured to the end of this night, not of the next one:
// Friday 23:00 with a window closing at 04:00 has five hours, even though
// Saturday has no window of its own.
func TestHowMuchIsLeftOfANightThatEndsOnADayWithoutOne(t *testing.T) {
	t.Parallel()

	env := weekdays(t, 22*60, 4*60, "UTC", time.Friday)

	if got, want := env.Window.RemainingAt(at(t, "2026-10-09T23:00:00Z")), 5*time.Hour; got != want {
		t.Errorf("friday 23:00: %v left, want %v", got, want)
	}
	if got, want := env.Window.RemainingAt(at(t, "2026-10-10T01:00:00Z")), 3*time.Hour; got != want {
		t.Errorf("saturday 01:00: %v left, want %v", got, want)
	}
	if got := env.Window.RemainingAt(at(t, "2026-10-10T05:00:00Z")); got != 0 {
		t.Errorf("saturday 05:00 is outside: %v left", got)
	}
}

// A run that would carry on after the night ends is refused, as before: the
// next night having a window of its own does not extend this one.
func TestARunCannotCrossFromOneNightIntoAnother(t *testing.T) {
	t.Parallel()

	env := weekdays(t, 22*60, 4*60, "UTC", time.Monday, time.Tuesday)

	// Monday 23:00 to Tuesday 05:00 would run an hour past the close.
	if err := env.RequireWindowAllows(at(t, "2026-10-05T23:00:00Z"), 6*time.Hour); !errors.Is(err, ErrOutsideAllowedWindow) {
		t.Fatalf("got %v", err)
	}
	if err := env.RequireWindowAllows(at(t, "2026-10-05T23:00:00Z"), 5*time.Hour); err != nil {
		t.Fatalf("a run that ends as the window does was refused: %v", err)
	}
}

func TestTheRefusalCitesTheDaysWhenThereAreSome(t *testing.T) {
	t.Parallel()

	env := weekdays(t, 22*60, 4*60, "Europe/Paris",
		time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday)

	err := env.RequireWindowAllows(at(t, "2026-10-10T12:00:00Z"), time.Minute)
	if !errors.Is(err, ErrOutsideAllowedWindow) {
		t.Fatalf("got %v", err)
	}
	for _, want := range []string{"22:00", "04:00", "mon–fri", "Europe/Paris"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal %q does not say %q", err, want)
		}
	}

	// And says nothing of days when there are none to say.
	every := windowed(t, 22*60, 4*60, "UTC")
	err = every.RequireWindowAllows(at(t, "2026-10-10T12:00:00Z"), time.Minute)
	if strings.Contains(err.Error(), "every day") || strings.Contains(err.Error(), "mon") {
		t.Errorf("the refusal of a window on every day talks about days: %q", err)
	}
}

func TestDaysAreNamedTheWayTheAPISpellsThem(t *testing.T) {
	t.Parallel()

	cases := map[string][]time.Weekday{
		"mon–fri":       {time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
		"sat–sun":       {time.Saturday, time.Sunday},
		"mon, wed, fri": {time.Monday, time.Wednesday, time.Friday},
		"fri":           {time.Friday},
		"fri–mon":       {time.Friday, time.Saturday, time.Sunday, time.Monday},
	}
	for want, days := range cases {
		if got := DaysOf(days...).String(); got != want {
			t.Errorf("%v is described as %q, want %q", days, got, want)
		}
	}
	if got := DaysOf().String(); got != "every day" {
		t.Errorf("no days is described as %q", got)
	}
}

func TestDaysRoundTripThroughTheirNames(t *testing.T) {
	t.Parallel()

	names := []string{"mon", "wed", "sun"}
	mask, err := ParseDays(names)
	if err != nil {
		t.Fatalf("ParseDays: %v", err)
	}
	if got := mask.Names(); strings.Join(got, ",") != "mon,wed,sun" {
		t.Errorf("Names: %v", got)
	}
	// In week order whatever order they came in, and a repeat is not an error.
	again, err := ParseDays([]string{"sun", "mon", "mon", "wed"})
	if err != nil || again != mask {
		t.Errorf("ParseDays out of order: %v %v", again, err)
	}
}

func TestDaysBallastWillNotStore(t *testing.T) {
	t.Parallel()

	for name, days := range map[string][]string{
		"a day that is not one": {"mon", "funday"},
		"a capital":             {"Mon"},
		"a full name":           {"monday"},
		"an empty list":         {},
	} {
		if _, err := ParseDays(days); !errors.Is(err, ErrInvalidEnvironment) {
			t.Errorf("%s: got %v", name, err)
		}
	}

	// A mask with a bit past Sunday set means something this code does not know.
	w := AllowedWindow{StartMinute: 60, EndMinute: 120, Zone: "UTC", Days: DayMask(1 << 7)}
	if err := w.Validate(); !errors.Is(err, ErrInvalidEnvironment) {
		t.Errorf("an unknown day bit was accepted: %v", err)
	}
}

// Every window written before days existed has none, and they keep meaning
// every day. The zero mask is that, not "no day at all".
func TestAWindowWrittenBeforeDaysExistedStillMeansEveryDay(t *testing.T) {
	t.Parallel()

	w := AllowedWindow{StartMinute: 22 * 60, EndMinute: 4 * 60, Zone: "UTC"}
	if err := w.Validate(); err != nil {
		t.Fatalf("an old window no longer validates: %v", err)
	}
	if !w.Contains(at(t, "2026-10-10T23:00:00Z")) {
		t.Error("an old window refused a Saturday night")
	}
}

// The night the clocks go forward in Paris (2026-03-29, 02:00 becomes 03:00).
// The night of Saturday 28th, 22:00 to 04:00, is one hour shorter than the
// clock says, and still belongs to Saturday.
func TestASaturdayNightAcrossDaylightSaving(t *testing.T) {
	t.Parallel()

	env := weekdays(t, 22*60, 4*60, "Europe/Paris", time.Saturday)

	// 23:00 Paris on Saturday is 22:00 UTC (still winter time).
	start := at(t, "2026-03-28T22:00:00Z")
	if !allowsAt(env, start) {
		t.Fatal("Saturday 23:00 in Paris was refused")
	}
	// Six hours of clock, five of time.
	if got, want := env.Window.RemainingAt(start), 4*time.Hour; got != want {
		t.Errorf("%v left on the night the clocks go forward, want %v", got, want)
	}
	// 03:00 Paris on Sunday the 29th is summer time: 01:00 UTC. Still the night
	// of Saturday, still inside.
	if !allowsAt(env, at(t, "2026-03-29T01:00:00Z")) {
		t.Error("Sunday 03:00 in Paris, the night of Saturday, was refused")
	}
	// 04:00 Paris is 02:00 UTC: closed.
	if allowsAt(env, at(t, "2026-03-29T02:00:00Z")) {
		t.Error("Sunday 04:00 in Paris was accepted")
	}
}

package domain

import (
	"errors"
	"testing"
	"time"
)

// A month is a period, and which period it is decides what a run is counted in.

func TestAMonthRunsFromItsFirstInstantToTheNextMonths(t *testing.T) {
	from, to, err := ParseMonth("2026-10")
	if err != nil {
		t.Fatalf("ParseMonth: %v", err)
	}

	if !from.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("october starts at %s", from)
	}
	if !to.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("october ends at %s", to)
	}
}

// Half-open, which is the whole point: a run that ended at the first instant of
// November belongs to November and to nothing else. Both ends included would
// have two months claiming it, and a total that double-counts is worse than one
// that is late.
func TestTheEndOfAMonthIsTheStartOfTheNext(t *testing.T) {
	_, october, _ := ParseMonth("2026-10")
	november, _, _ := ParseMonth("2026-11")

	if !october.Equal(november) {
		t.Fatalf("october ends at %s and november starts at %s", october, november)
	}
}

// December rolls over to the next year rather than to month thirteen.
func TestDecemberRollsOver(t *testing.T) {
	_, to, err := ParseMonth("2026-12")
	if err != nil {
		t.Fatalf("ParseMonth: %v", err)
	}
	if !to.Equal(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("december ends at %s", to)
	}
}

func TestWhatIsNotAMonthIsRefused(t *testing.T) {
	for _, raw := range []string{"", "2026", "2026-13", "octobre", "2026-10-01", "26-10"} {
		if _, _, err := ParseMonth(raw); !errors.Is(err, ErrInvalidMonth) {
			t.Errorf("%q was read as a month: %v", raw, err)
		}
	}
}

// The month an instant falls in, in UTC — whatever the clock of whoever asks.
func TestTheMonthOfAnInstantIsItsUTCMonth(t *testing.T) {
	// Half past midnight in Paris on the first of November is still October in
	// UTC, and that is the month this run is counted in.
	paris := time.FixedZone("CEST", 2*60*60)
	from, to := MonthOf(time.Date(2026, 11, 1, 0, 30, 0, 0, paris))

	if !from.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("the month runs from %s to %s", from, to)
	}
}

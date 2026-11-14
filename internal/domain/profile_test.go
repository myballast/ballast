package domain

import (
	"errors"
	"testing"
)

var allProfileKinds = []ProfileKind{
	ProfileSmoke,
	ProfileLoad,
	ProfileStress,
	ProfileSpike,
	ProfileSoak,
}

// Every combination the sweeps below run through. Small, but it covers the
// shapes that matter: zero, one, something ordinary, something far above any
// ceiling.
var (
	sweepVUs      = []int{1, 5, 50, 1000}
	sweepRampUp   = []int{0, 1, 30, 600}
	sweepPlateau  = []int{0, 1, 60, 3600}
	sweepRampDown = []int{0, 1, 30, 600}
	// Ceilings are always positive: migration 001 constrains max_vus > 0 and
	// max_duration_s > 0.
	sweepMaxVUs      = []int{1, 5, 50}
	sweepMaxDuration = []int{1, 60, 600}
)

// eachCase walks the sweep and hands every pair to fn.
func eachCase(fn func(p ProfileParams, env Environment)) {
	for _, vus := range sweepVUs {
		for _, up := range sweepRampUp {
			for _, plateau := range sweepPlateau {
				for _, down := range sweepRampDown {
					for _, maxVUs := range sweepMaxVUs {
						for _, maxDuration := range sweepMaxDuration {
							fn(
								ProfileParams{TargetVUs: vus, RampUpS: up, PlateauS: plateau, RampDownS: down},
								Environment{MaxVUs: maxVUs, MaxDurationS: maxDuration},
							)
						}
					}
				}
			}
		}
	}
}

func TestTotalDuration(t *testing.T) {
	cases := []struct {
		name string
		p    ProfileParams
		want int
	}{
		{"all zero", ProfileParams{}, 0},
		{"plateau only", ProfileParams{PlateauS: 60}, 60},
		{"three phases", ProfileParams{RampUpS: 30, PlateauS: 120, RampDownS: 10}, 160},
		{"virtual users do not count", ProfileParams{TargetVUs: 500, PlateauS: 1}, 1},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.p.TotalDuration(); got != c.want {
				t.Errorf("TotalDuration() = %d, want %d", got, c.want)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		p       ProfileParams
		wantErr bool
	}{
		{"a smoke test", ProfileParams{TargetVUs: 1, PlateauS: 60}, false},
		{"every phase used", ProfileParams{TargetVUs: 50, RampUpS: 30, PlateauS: 60, RampDownS: 30}, false},
		{"one second, one user", ProfileParams{TargetVUs: 1, PlateauS: 1}, false},

		{"no virtual users", ProfileParams{TargetVUs: 0, PlateauS: 60}, true},
		{"negative virtual users", ProfileParams{TargetVUs: -1, PlateauS: 60}, true},
		{"negative ramp-up", ProfileParams{TargetVUs: 1, RampUpS: -1, PlateauS: 60}, true},
		{"negative plateau", ProfileParams{TargetVUs: 1, PlateauS: -1, RampDownS: 60}, true},
		{"negative ramp-down", ProfileParams{TargetVUs: 1, PlateauS: 60, RampDownS: -1}, true},
		{"no duration at all", ProfileParams{TargetVUs: 1}, true},
		{"phases cancelling out", ProfileParams{TargetVUs: 1, RampUpS: 60, PlateauS: -60}, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.p.Validate()

			if c.wantErr && err == nil {
				t.Fatal("Validate() accepted parameters that describe no runnable test")
			}
			if !c.wantErr && err != nil {
				t.Fatalf("Validate() rejected a runnable test: %v", err)
			}
			if err != nil && !errors.Is(err, ErrInvalidProfileParams) {
				t.Errorf("error does not wrap ErrInvalidProfileParams: %v", err)
			}
		})
	}
}

func TestClampToBoundaries(t *testing.T) {
	env := Environment{MaxVUs: 50, MaxDurationS: 600}

	cases := []struct {
		name string
		p    ProfileParams
		want ProfileParams
	}{
		{
			"below every ceiling, nothing moves",
			ProfileParams{TargetVUs: 10, RampUpS: 30, PlateauS: 60, RampDownS: 30},
			ProfileParams{TargetVUs: 10, RampUpS: 30, PlateauS: 60, RampDownS: 30},
		},
		{
			"exactly at both ceilings, nothing moves",
			ProfileParams{TargetVUs: 50, RampUpS: 100, PlateauS: 400, RampDownS: 100},
			ProfileParams{TargetVUs: 50, RampUpS: 100, PlateauS: 400, RampDownS: 100},
		},
		{
			"one user over the ceiling",
			ProfileParams{TargetVUs: 51, PlateauS: 60},
			ProfileParams{TargetVUs: 50, PlateauS: 60},
		},
		{
			"one second over the ceiling, the ramp-down pays",
			ProfileParams{TargetVUs: 10, RampUpS: 100, PlateauS: 400, RampDownS: 101},
			ProfileParams{TargetVUs: 10, RampUpS: 100, PlateauS: 400, RampDownS: 100},
		},
		{
			"the plateau pays once the ramp-down is gone",
			ProfileParams{TargetVUs: 10, RampUpS: 100, PlateauS: 5000, RampDownS: 100},
			ProfileParams{TargetVUs: 10, RampUpS: 100, PlateauS: 500, RampDownS: 0},
		},
		{
			// The case a plain subtraction gets wrong: it
			// would have left a ramp-down of 600 - 900 - 900 = -1200.
			"the ramp-up alone exceeds the ceiling",
			ProfileParams{TargetVUs: 10, RampUpS: 900, PlateauS: 900, RampDownS: 900},
			ProfileParams{TargetVUs: 10, RampUpS: 600, PlateauS: 0, RampDownS: 0},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.p.ClampTo(env); got != c.want {
				t.Errorf("ClampTo() = %+v, want %+v", got, c.want)
			}
		})
	}
}

// The safety net. Whatever goes in, what comes out fits inside the ceilings —
// and a test that was runnable before clamping is still runnable after, which
// is the part that matters: clamping must never be the reason a run cannot
// happen.
//
// Nothing is claimed about parameters that were already invalid. Repairing
// those is Validate's job, not ClampTo's; the ceilings alone are held for them,
// which TestClampToHoldsTheCeilingEvenForInvalidInput covers.
func TestClampToAlwaysFitsInsideTheCeilings(t *testing.T) {
	eachCase(func(p ProfileParams, env Environment) {
		got := p.ClampTo(env)
		runnableBefore := p.Validate() == nil

		if got.TargetVUs > env.MaxVUs {
			t.Fatalf("%+v clamped to %+v: %d virtual users exceeds the ceiling of %d",
				p, got, got.TargetVUs, env.MaxVUs)
		}
		if got.TotalDuration() > env.MaxDurationS {
			t.Fatalf("%+v clamped to %+v: %ds exceeds the ceiling of %ds",
				p, got, got.TotalDuration(), env.MaxDurationS)
		}
		if got.RampUpS < 0 || got.PlateauS < 0 || got.RampDownS < 0 {
			t.Fatalf("%+v clamped to %+v: a phase went negative", p, got)
		}
		if err := got.Validate(); runnableBefore && err != nil {
			t.Fatalf("%+v was runnable, clamped to %+v, which is not: %v", p, got, err)
		}
	})
}

// Clamping lowers. It must never hand back something larger than it was given.
func TestClampToNeverRaisesAnything(t *testing.T) {
	eachCase(func(p ProfileParams, env Environment) {
		got := p.ClampTo(env)

		switch {
		case got.TargetVUs > p.TargetVUs,
			got.RampUpS > p.RampUpS,
			got.PlateauS > p.PlateauS,
			got.RampDownS > p.RampDownS:
			t.Fatalf("%+v clamped to %+v, which is larger somewhere", p, got)
		}
	})
}

func TestClampToIsIdempotent(t *testing.T) {
	eachCase(func(p ProfileParams, env Environment) {
		once := p.ClampTo(env)
		if twice := once.ClampTo(env); twice != once {
			t.Fatalf("%+v clamped to %+v, then to %+v", p, once, twice)
		}
	})
}

// Parameters that were already invalid are not ClampTo's to repair, but it must
// not let them through the ceiling either.
func TestClampToHoldsTheCeilingEvenForInvalidInput(t *testing.T) {
	env := Environment{MaxVUs: 10, MaxDurationS: 60}

	for _, p := range []ProfileParams{
		{TargetVUs: 1000, RampUpS: -100, PlateauS: 10000, RampDownS: 10000},
		{TargetVUs: -5, RampUpS: 10000, PlateauS: -10000, RampDownS: 10000},
		{TargetVUs: 0, RampUpS: -1, PlateauS: -1, RampDownS: -1},
	} {
		got := p.ClampTo(env)

		if got.TargetVUs > env.MaxVUs {
			t.Errorf("%+v clamped to %+v: %d virtual users exceeds %d",
				p, got, got.TargetVUs, env.MaxVUs)
		}
		if got.TotalDuration() > env.MaxDurationS {
			t.Errorf("%+v clamped to %+v: %ds exceeds %ds",
				p, got, got.TotalDuration(), env.MaxDurationS)
		}
	}
}

func TestDefaultsAreDefinedAndRunnable(t *testing.T) {
	for _, kind := range allProfileKinds {
		t.Run(string(kind), func(t *testing.T) {
			p, ok := kind.Defaults()
			if !ok {
				t.Fatalf("%s has no template", kind)
			}
			if err := p.Validate(); err != nil {
				t.Errorf("the %s template is not runnable: %v", kind, err)
			}
		})
	}

	if _, ok := ProfileKind("chaos").Defaults(); ok {
		t.Error("an unknown profile returned a template")
	}
}

// These strings are stored in run.profile and scenario.default_profile, and
// constrained by migration 001. A typo here is a row the database refuses.
func TestProfileKindValuesMatchTheSchema(t *testing.T) {
	want := map[ProfileKind]string{
		ProfileSmoke:  "smoke",
		ProfileLoad:   "load",
		ProfileStress: "stress",
		ProfileSpike:  "spike",
		ProfileSoak:   "soak",
	}

	if len(want) != len(allProfileKinds) || len(want) != len(defaultProfiles) {
		t.Fatalf("%d values pinned, %d kinds listed, %d templates defined",
			len(want), len(allProfileKinds), len(defaultProfiles))
	}

	for kind, literal := range want {
		if string(kind) != literal {
			t.Errorf("profile constant is %q, the schema expects %q", string(kind), literal)
		}
	}
}

package domain

import "fmt"

// ProfileKind is one of the five load templates. A user picks one instead of
// writing stages by hand; the script itself never changes.
//
// The strings are the values stored in run.profile and scenario.default_profile
// and constrained by migration 001. They are part of the schema.
type ProfileKind string

// The five templates a run can be launched with.
const (
	ProfileSmoke  ProfileKind = "smoke"
	ProfileLoad   ProfileKind = "load"
	ProfileStress ProfileKind = "stress"
	ProfileSpike  ProfileKind = "spike"
	ProfileSoak   ProfileKind = "soak"
)

// ProfileParams is the shape of the load for one run: how many virtual users,
// and how long each phase lasts.
//
// Durations are whole seconds, not time.Duration. That is the unit of
// environment.max_duration_s, of the profile_params jsonb, and of the k6
// options generated from it — one unit from end to end, with no conversion to
// get wrong at a boundary.
type ProfileParams struct {
	// TargetVUs is the number of virtual users the plateau holds.
	TargetVUs int
	// RampUpS climbs from zero to TargetVUs.
	RampUpS int
	// PlateauS holds TargetVUs.
	PlateauS int
	// RampDownS returns to zero.
	RampDownS int
}

// The five templates, hardcoded. They do not change, and they are starting
// points: whatever a user picks is clamped against the environment's ceilings
// before it ever reaches a run.
//
// The shapes come from the load profiles of the cahier des charges, section
// 4.3.
var defaultProfiles = map[ProfileKind]ProfileParams{
	// One user for a minute: does it answer at all?
	ProfileSmoke: {TargetVUs: 1, RampUpS: 0, PlateauS: 60, RampDownS: 0},
	// A progressive climb to nominal load, then a sustained plateau.
	ProfileLoad: {TargetVUs: 50, RampUpS: 120, PlateauS: 600, RampDownS: 60},
	// A long climb, looking for where it breaks. The plateau is short: what
	// matters is the ascent, not holding whatever it survived.
	ProfileStress: {TargetVUs: 200, RampUpS: 600, PlateauS: 120, RampDownS: 60},
	// A brutal peak and back, which is what a marketing email looks like.
	ProfileSpike: {TargetVUs: 200, RampUpS: 10, PlateauS: 60, RampDownS: 10},
	// Moderate load held for hours, where leaks show up.
	ProfileSoak: {TargetVUs: 20, RampUpS: 300, PlateauS: 10800, RampDownS: 300},
}

// Defaults returns the template for kind, and whether kind is one of the five.
func (k ProfileKind) Defaults() (ProfileParams, bool) {
	p, ok := defaultProfiles[k]
	return p, ok
}

// TotalDuration is how long the run lasts, in seconds.
func (p ProfileParams) TotalDuration() int {
	return p.RampUpS + p.PlateauS + p.RampDownS
}

// Validate reports why these parameters describe no runnable test.
func (p ProfileParams) Validate() error {
	switch {
	case p.TargetVUs <= 0:
		return fmt.Errorf("%w: target VUs is %d, want at least 1", ErrInvalidProfileParams, p.TargetVUs)
	case p.RampUpS < 0:
		return fmt.Errorf("%w: ramp-up is %ds, want zero or more", ErrInvalidProfileParams, p.RampUpS)
	case p.PlateauS < 0:
		return fmt.Errorf("%w: plateau is %ds, want zero or more", ErrInvalidProfileParams, p.PlateauS)
	case p.RampDownS < 0:
		return fmt.Errorf("%w: ramp-down is %ds, want zero or more", ErrInvalidProfileParams, p.RampDownS)
	case p.TotalDuration() <= 0:
		return fmt.Errorf("%w: the run would last %ds, want at least 1s", ErrInvalidProfileParams, p.TotalDuration())
	}
	return nil
}

// ClampTo lowers these parameters until they fit inside the ceilings that
// apply: the environment's, and the ones no configuration can raise. This is what keeps Ballast from generating more load than a target
// was declared able to take, so it runs on every path to a run, without
// exception.
//
// Duration is clamped phase by phase against a shrinking budget rather than by
// subtracting the other phases from the ceiling. Subtraction is the obvious way
// to write it and it is wrong: a ramp-up that already exceeds the ceiling makes
// the ramp-down negative, and a negative stage goes straight into the generated
// k6 options. Spending a budget cannot produce one.
//
// The order is what gets sacrificed first: the ramp-down goes before the
// plateau, and the plateau before the climb. A test that never reaches its
// target load measures nothing.
//
// ClampTo only ever lowers. It does not repair parameters that were already
// invalid — that is what Validate is for — but it never turns valid ones into
// invalid ones either, beyond what a degenerate ceiling forces.
func (p ProfileParams) ClampTo(env Environment) ProfileParams {
	// The smaller of the two ceilings, always. What an environment declares is
	// what a target was said to tolerate; what MaxVUsAnywhere declares is what
	// this installation can generate without measuring itself instead. Either
	// one being generous is no reason to ignore the other, and an environment
	// configured with ten thousand virtual users is a mistake rather than a
	// permission.
	vus := min(env.MaxVUs, MaxVUsAnywhere)
	if p.TargetVUs > vus {
		p.TargetVUs = vus
	}

	budget := min(env.MaxDurationS, MaxDurationAnywhereS)
	p.RampUpS = spend(p.RampUpS, &budget)
	p.PlateauS = spend(p.PlateauS, &budget)
	p.RampDownS = spend(p.RampDownS, &budget)

	return p
}

// spend takes seconds out of budget, or as much of it as is left.
func spend(seconds int, budget *int) int {
	if seconds > *budget {
		seconds = *budget
	}
	*budget -= seconds

	return seconds
}

package domain

import (
	"testing"
	"time"
)

// The ceilings nobody can raise from the outside, and what they do to a run
// whose environment was configured without reading them.

func TestAnEnvironmentCannotAllowMoreThanTheInstallationCan(t *testing.T) {
	// Somebody typed ten thousand into a form. That is a mistake, not a
	// permission: the generator container cannot drive it, and what such a run
	// would measure is its own saturation.
	generous := Environment{MaxVUs: 10000, MaxDurationS: 86400}

	clamped := ProfileParams{
		TargetVUs: 5000,
		RampUpS:   600,
		PlateauS:  80000,
		RampDownS: 600,
	}.ClampTo(generous)

	if clamped.TargetVUs != MaxVUsAnywhere {
		t.Errorf("%d virtual users survived a ceiling of %d", clamped.TargetVUs, MaxVUsAnywhere)
	}
	if total := clamped.TotalDuration(); total != MaxDurationAnywhereS {
		t.Errorf("the run lasts %ds, the ceiling is %ds", total, MaxDurationAnywhereS)
	}
}

// The environment's ceiling still wins when it is the smaller one. The hard
// limit is a floor under carelessness, not a licence to ignore what a target
// said it could take.
func TestTheSmallerCeilingIsTheOneThatApplies(t *testing.T) {
	careful := Environment{MaxVUs: 5, MaxDurationS: 30}

	clamped := ProfileParams{TargetVUs: 150, PlateauS: 600}.ClampTo(careful)

	if clamped.TargetVUs != 5 {
		t.Errorf("a target that tolerates 5 virtual users was given %d", clamped.TargetVUs)
	}
	if total := clamped.TotalDuration(); total != 30 {
		t.Errorf("a target that tolerates 30s was given %ds", total)
	}
}

// A run that fits inside both is left exactly as it was asked for. A clamp
// that lowered what it had no reason to lower would make every run a
// negotiation.
func TestARunInsideBothCeilingsIsUntouched(t *testing.T) {
	asked := ProfileParams{TargetVUs: 10, RampUpS: 30, PlateauS: 120, RampDownS: 30}

	clamped := asked.ClampTo(Environment{MaxVUs: 50, MaxDurationS: 600})

	if clamped != asked {
		t.Fatalf("the run was asked for %+v and became %+v", asked, clamped)
	}
}

// The two spellings of the duration ceiling must say the same thing. One is
// used by the clamp, the other by whoever compares it to a timeout.
func TestTheDurationCeilingIsTheSameInBothUnits(t *testing.T) {
	if time.Duration(MaxDurationAnywhereS)*time.Second != MaxDurationAnywhere {
		t.Fatalf("%ds and %s are not the same ceiling", MaxDurationAnywhereS, MaxDurationAnywhere)
	}
}

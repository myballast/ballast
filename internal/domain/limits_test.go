package domain

import (
	"encoding/json"
	"os"
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

// What a run can really use on an environment: the smaller of what it declares and
// what no configuration can raise. The interface says it, so that it does not hold
// a copy of either number.
func TestAnEnvironmentSaysWhatARunCanUseOnIt(t *testing.T) {
	cases := map[string]struct {
		env     Environment
		vus     int
		seconds int
	}{
		"a prudent environment stays prudent": {Environment{MaxVUs: 10, MaxDurationS: 60}, 10, 60},
		"a mistake is not a permission":       {Environment{MaxVUs: 10000, MaxDurationS: 999999}, MaxVUsAnywhere, MaxDurationAnywhereS},
		"one of each":                         {Environment{MaxVUs: 10000, MaxDurationS: 60}, MaxVUsAnywhere, 60},
	}

	for name, c := range cases {
		vus, seconds := c.env.Ceilings()
		if vus != c.vus || seconds != c.seconds {
			t.Errorf("%s: %d VUs and %d s, wanted %d and %d", name, vus, seconds, c.vus, c.seconds)
		}
	}
}

// The front end shows what a profile will be lowered to, with its own copy of the
// arithmetic. Both are asked the same questions (web/src/features/builder/
// clamp.test.ts reads the same file), so that what a person is told before a run is
// what the run is given.
func TestTheSharedClampCorpusGivesWhatIsWritten(t *testing.T) {
	raw, err := os.ReadFile("testdata/clamp.json")
	if err != nil {
		t.Fatalf("read the corpus: %v", err)
	}

	type params struct {
		TargetVUs int `json:"target_vus"`
		RampUpS   int `json:"ramp_up_s"`
		PlateauS  int `json:"plateau_s"`
		RampDownS int `json:"ramp_down_s"`
	}
	var corpus struct {
		Cases []struct {
			Name       string `json:"name"`
			Params     params `json:"params"`
			CeilingVUs int    `json:"ceiling_vus"`
			CeilingS   int    `json:"ceiling_s"`
			Want       params `json:"want"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatalf("decode the corpus: %v", err)
	}
	if len(corpus.Cases) < 5 {
		t.Fatal("the corpus is too small to prove anything")
	}

	for _, c := range corpus.Cases {
		got := ProfileParams{
			TargetVUs: c.Params.TargetVUs, RampUpS: c.Params.RampUpS,
			PlateauS: c.Params.PlateauS, RampDownS: c.Params.RampDownS,
		}.ClampTo(Environment{MaxVUs: c.CeilingVUs, MaxDurationS: c.CeilingS})

		want := ProfileParams{
			TargetVUs: c.Want.TargetVUs, RampUpS: c.Want.RampUpS,
			PlateauS: c.Want.PlateauS, RampDownS: c.Want.RampDownS,
		}
		if got != want {
			t.Errorf("%s: got %+v, wanted %+v", c.Name, got, want)
		}
	}
}

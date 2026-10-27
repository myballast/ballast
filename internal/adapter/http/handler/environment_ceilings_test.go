package handler

import (
	"testing"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// The front end tells somebody what a profile will be lowered to, and it must not
// carry a copy of the installation's ceilings to do it: the API says them, as what
// a run can use on this environment, not as what the environment declares.
func TestAnEnvironmentSaysWhatARunCanUseOnIt(t *testing.T) {
	declared := environmentOf(domain.Environment{MaxVUs: 10000, MaxDurationS: 999999})
	if declared.MaxVUs != 10000 || declared.MaxDurationS != 999999 {
		t.Errorf("what it declares changed: %d and %d", declared.MaxVUs, declared.MaxDurationS)
	}
	if declared.EffectiveMaxVUs != domain.MaxVUsAnywhere || declared.EffectiveMaxDurationS != domain.MaxDurationAnywhereS {
		t.Errorf("a mistake was shown as a permission: %d VUs and %d s",
			declared.EffectiveMaxVUs, declared.EffectiveMaxDurationS)
	}

	careful := environmentOf(domain.Environment{MaxVUs: 10, MaxDurationS: 60})
	if careful.EffectiveMaxVUs != 10 || careful.EffectiveMaxDurationS != 60 {
		t.Errorf("a prudent environment was shown as more: %d and %d", careful.EffectiveMaxVUs, careful.EffectiveMaxDurationS)
	}
}

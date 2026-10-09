package domain

import (
	"errors"
	"strings"
	"testing"
)

// A secret becomes an environment variable of the generator's container, and k6
// reads its own configuration from environment variables with a higher
// precedence than the options of the module it runs. Ballast's ceilings live in
// those options. So a key is not just a name: some names are instructions.
func TestAKeyThatWouldReconfigureTheGeneratorIsRefused(t *testing.T) {
	t.Parallel()

	refused := map[string]string{
		"K6_VUS":                      "would raise the virtual users past the environment's ceiling",
		"K6_DURATION":                 "would run longer than the environment allows",
		"K6_ITERATIONS":               "would replace the load the profile describes",
		"K6_OUT":                      "would stream the metrics to an endpoint of the user's choosing",
		"K6_INSECURE_SKIP_TLS_VERIFY": "would turn off certificate checking",
		"K6_NO_THRESHOLDS":            "would make every threshold pass",
	}

	for key, why := range refused {
		t.Run(key, func(t *testing.T) {
			t.Parallel()

			if err := ValidateSecretKey(key); !errors.Is(err, ErrInvalidSecret) {
				t.Fatalf("%s was accepted, and it %s: %v", key, why, err)
			}
		})
	}
}

// Ballast injects BASE_URL itself, from the environment whose ownership was
// proved and approved. A secret of that name is an attempt to send the load
// somewhere else.
func TestAKeyBallastInjectsItselfIsRefused(t *testing.T) {
	t.Parallel()

	for _, key := range ReservedSecretKeys() {
		if err := ValidateSecretKey(key); !errors.Is(err, ErrInvalidSecret) {
			t.Fatalf("%s is injected by Ballast and was accepted as a secret: %v", key, err)
		}
	}
}

func TestAKeyThatIsNotAnEnvironmentVariableNameIsRefused(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"empty":           "",
		"blank":           "   ",
		"lower case":      "api_token",
		"leading digit":   "1TOKEN",
		"leading score":   "_TOKEN",
		"a dash":          "API-TOKEN",
		"an equals sign":  "API=TOKEN",
		"a space":         "API TOKEN",
		"a newline":       "API\nTOKEN",
		"a null byte":     "API\x00TOKEN",
		"too long":        strings.Repeat("A", 129),
		"not even ascii":  "JETON_Ñ",
		"shell expansion": "$PATH",
		"path traversal":  "../TOKEN",
	}

	for name, key := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := ValidateSecretKey(key); !errors.Is(err, ErrInvalidSecret) {
				t.Fatalf("%q was accepted as a key: %v", key, err)
			}
		})
	}
}

func TestAnOrdinaryKeyIsAccepted(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"API_TOKEN", "T", "AUTH_BEARER_2", "BALLASTIC"} {
		if err := ValidateSecretKey(key); err != nil {
			t.Fatalf("%q should be a usable key: %v", key, err)
		}
	}
}

// The prefix is refused, not a list of the variables k6 reads today. A list is a
// thing to forget the day k6 adds one.
func TestTheWholeK6PrefixIsRefusedRatherThanAListOfNames(t *testing.T) {
	t.Parallel()

	if err := ValidateSecretKey("K6_SOMETHING_INVENTED_IN_A_LATER_RELEASE"); !errors.Is(err, ErrInvalidSecret) {
		t.Fatalf("a K6_ variable nobody has heard of yet was accepted: %v", err)
	}
}

func TestAValueTooLargeToBeACredentialIsRefused(t *testing.T) {
	t.Parallel()

	if err := ValidateSecretValue(strings.Repeat("x", 4097)); !errors.Is(err, ErrInvalidSecret) {
		t.Fatalf("a value of 4097 bytes was accepted: %v", err)
	}
	if err := ValidateSecretValue(""); !errors.Is(err, ErrInvalidSecret) {
		t.Fatalf("an empty value was accepted: %v", err)
	}
	if err := ValidateSecretValue("a-perfectly-ordinary-token"); err != nil {
		t.Fatalf("an ordinary token was refused: %v", err)
	}
}

// A value is passed to the kernel as one element of the container's environment,
// where a newline is not a separator and a null byte ends the string. Refusing
// them here is cheaper than reasoning about what Docker does with them.
func TestAValueCarryingAControlCharacterIsRefused(t *testing.T) {
	t.Parallel()

	for name, value := range map[string]string{
		"a null byte": "token\x00more",
		"a newline":   "token\nmore",
	} {
		if err := ValidateSecretValue(value); !errors.Is(err, ErrInvalidSecret) {
			t.Fatalf("a value with %s was accepted", name)
		}
	}
}

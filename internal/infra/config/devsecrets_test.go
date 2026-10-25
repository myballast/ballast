package config

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

const (
	remoteDSN = "postgres://ballast_app:pw@postgres.railway.internal:5432/railway?sslmode=disable"
	localDSN  = "postgres://ballast_app:ballast_app@127.0.0.1:5432/ballast?sslmode=disable"
	realKey   = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="
)

func apiEnv(t *testing.T, dsn, key, session, publicURL string) {
	t.Helper()
	t.Setenv("BALLAST_DATABASE_URL", dsn)
	t.Setenv("BALLAST_SECRET_KEY", key)
	t.Setenv("BALLAST_SESSION_SECRET", session)
	t.Setenv("BALLAST_PUBLIC_URL", publicURL)
	t.Setenv("BALLAST_WEB_URL", publicURL)
}

func TestTheDevelopmentSecretKeyIsRefusedOnADatabaseThatIsNotLocal(t *testing.T) {
	t.Setenv("BALLAST_DATABASE_URL", remoteDSN)
	t.Setenv("BALLAST_SECRET_KEY", developmentSecretKey)

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "BALLAST_SECRET_KEY") {
		t.Fatalf("Load = %v, want a refusal naming BALLAST_SECRET_KEY", err)
	}
	// The refusal says what to do, and never repeats the key.
	if strings.Contains(err.Error(), developmentSecretKey) {
		t.Error("the refusal repeats the key")
	}
}

func TestADevelopmentKeyIsFineOnALocalDatabase(t *testing.T) {
	for _, dsn := range []string{
		localDSN,
		"postgres://u:p@localhost:5432/d",
		"postgres://u:p@[::1]:5432/d",
	} {
		t.Setenv("BALLAST_DATABASE_URL", dsn)
		t.Setenv("BALLAST_SECRET_KEY", developmentSecretKey)

		if _, err := Load(); err != nil {
			t.Errorf("Load with %q refused the development key: %v", dsn, err)
		}
	}
}

func TestARealKeyIsAcceptedOnARemoteDatabase(t *testing.T) {
	t.Setenv("BALLAST_DATABASE_URL", remoteDSN)
	t.Setenv("BALLAST_SECRET_KEY", realKey)

	if _, err := Load(); err != nil {
		t.Errorf("Load refused a key that is not the development one: %v", err)
	}
}

func TestADatabaseAddressThatCannotBeReadIsNotTakenForLocal(t *testing.T) {
	t.Setenv("BALLAST_DATABASE_URL", "not a url ://")
	t.Setenv("BALLAST_SECRET_KEY", developmentSecretKey)

	if _, err := Load(); err == nil {
		t.Error("an unreadable database address let the development key through")
	}
}

func TestTheDevelopmentSessionSecretIsRefusedOnAPublicAddress(t *testing.T) {
	apiEnv(t, remoteDSN, realKey, developmentSessionSecret, "https://front.example.test")

	_, err := LoadAPI()
	if err == nil || !strings.Contains(err.Error(), "BALLAST_SESSION_SECRET") {
		t.Fatalf("LoadAPI = %v, want a refusal naming BALLAST_SESSION_SECRET", err)
	}
	if strings.Contains(err.Error(), developmentSessionSecret) {
		t.Error("the refusal repeats the secret")
	}
}

func TestAPublicAddressMustBeHTTPS(t *testing.T) {
	apiEnv(t, remoteDSN, realKey, "a-real-session-secret-of-enough-length", "http://front.example.test")

	_, err := LoadAPI()
	if err == nil || !strings.Contains(err.Error(), "BALLAST_PUBLIC_URL") {
		t.Errorf("LoadAPI = %v, want a refusal naming BALLAST_PUBLIC_URL: the session cookie is Secure", err)
	}
}

func TestEverythingDevelopmentIsFineOnLoopback(t *testing.T) {
	apiEnv(t, localDSN, developmentSecretKey, developmentSessionSecret, "http://localhost:8080")

	if _, err := LoadAPI(); err != nil {
		t.Errorf("LoadAPI refused the development setup on this machine: %v", err)
	}
}

func TestAProductionSetupIsAccepted(t *testing.T) {
	apiEnv(t, remoteDSN, realKey, "a-real-session-secret-of-enough-length", "https://front.example.test")

	if _, err := LoadAPI(); err != nil {
		t.Errorf("LoadAPI refused a setup with its own secrets: %v", err)
	}
}

// The development values are written twice: here, where they are refused, and in
// the Makefile, where they are the default. A copy that drifts would refuse
// nothing, and nobody would notice until the day it mattered.
func TestTheKnownDevelopmentSecretsAreTheMakefilesDefaults(t *testing.T) {
	makefile, err := os.ReadFile("../../../Makefile")
	if err != nil {
		t.Fatalf("read the Makefile: %v", err)
	}

	for name, want := range map[string]string{
		"DEV_SECRET_KEY :=": developmentSecretKey,
	} {
		if !strings.Contains(string(makefile), name+" "+want) {
			t.Errorf("the Makefile no longer has %q followed by the value this package refuses", name)
		}
	}

	pattern := regexp.MustCompile(`BALLAST_SESSION_SECRET \?= (\S+)`)
	match := pattern.FindStringSubmatch(string(makefile))
	if match == nil || match[1] != developmentSessionSecret {
		t.Errorf("the Makefile's default session secret is not the one this package refuses (found %v)", match)
	}
}

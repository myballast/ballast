package config

import (
	"fmt"
	"strings"
	"testing"
)

func setStorageEnv(t *testing.T) {
	t.Helper()
	t.Setenv("BALLAST_S3_ENDPOINT", "bucket.railway.internal:9000")
	t.Setenv("BALLAST_S3_BUCKET", "ballast")
	t.Setenv("BALLAST_S3_ACCESS_KEY", "ballast-app")
	t.Setenv("BALLAST_S3_SECRET_KEY", "a-very-secret-key")
	t.Setenv("BALLAST_S3_USE_SSL", "")
}

func TestLoadStorageReadsTheFourSettings(t *testing.T) {
	setStorageEnv(t)

	cfg, err := LoadStorage()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Endpoint != "bucket.railway.internal:9000" || cfg.Bucket != "ballast" ||
		cfg.AccessKey != "ballast-app" || cfg.SecretKey != "a-very-secret-key" {
		t.Errorf("unexpected configuration: %+v", cfg)
	}
}

func TestLoadStorageRefusesToGuessAnything(t *testing.T) {
	for _, missing := range []string{
		"BALLAST_S3_ENDPOINT", "BALLAST_S3_BUCKET", "BALLAST_S3_ACCESS_KEY", "BALLAST_S3_SECRET_KEY",
	} {
		t.Run(missing, func(t *testing.T) {
			setStorageEnv(t)
			t.Setenv(missing, "")

			_, err := LoadStorage()
			if err == nil || !strings.Contains(err.Error(), missing) {
				t.Errorf("LoadStorage without %s = %v, want an error naming it", missing, err)
			}
		})
	}
}

// A connection in clear text is a decision, not a default: the endpoint may be
// on the other side of the internet.
func TestLoadStorageUsesTLSUnlessToldOtherwise(t *testing.T) {
	setStorageEnv(t)

	cfg, _ := LoadStorage()
	if !cfg.UseSSL {
		t.Error("TLS is off by default")
	}

	t.Setenv("BALLAST_S3_USE_SSL", "false")
	cfg, _ = LoadStorage()
	if cfg.UseSSL {
		t.Error("BALLAST_S3_USE_SSL=false was not honoured")
	}

	t.Setenv("BALLAST_S3_USE_SSL", "perhaps")
	if _, err := LoadStorage(); err == nil {
		t.Error("an unreadable BALLAST_S3_USE_SSL was read as a value")
	}
}

func TestLoadStorageRefusesAnEndpointThatIsAnURL(t *testing.T) {
	setStorageEnv(t)

	for _, endpoint := range []string{"https://bucket.example.test", "bucket.example.test/path", "bucket example"} {
		t.Setenv("BALLAST_S3_ENDPOINT", endpoint)
		if _, err := LoadStorage(); err == nil {
			t.Errorf("endpoint %q was accepted", endpoint)
		}
	}
}

func TestTheSecretKeyNeverReachesALog(t *testing.T) {
	setStorageEnv(t)
	cfg, _ := LoadStorage()

	for _, printed := range []string{cfg.String(), strings.TrimSpace(sprint(cfg)), sprintf("%+v", cfg), sprintf("%#v", cfg)} {
		if strings.Contains(printed, "a-very-secret-key") {
			t.Errorf("the secret key was printed: %s", printed)
		}
	}
}

func sprint(v any) string            { return fmt.Sprint(v) }
func sprintf(f string, v any) string { return fmt.Sprintf(f, v) }

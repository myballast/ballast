package domain

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestAnArtifactKeyIsPrefixedByOrganizationAndRun(t *testing.T) {
	org, run := uuid.New(), uuid.New()

	got, err := ArtifactKey(org, run, "summary.json")
	if err != nil {
		t.Fatalf("ArtifactKey: %v", err)
	}

	want := "org/" + org.String() + "/run/" + run.String() + "/summary.json"
	if got != want {
		t.Errorf("key = %q, want %q", got, want)
	}
}

func TestTwoOrganizationsNeverShareAKeyPrefix(t *testing.T) {
	run := uuid.New()

	a, _ := ArtifactKey(uuid.New(), run, "summary.json")
	b, _ := ArtifactKey(uuid.New(), run, "summary.json")

	// The same run identifier under two organizations is two keys: the prefix
	// is what keeps the objects apart, not the run's uniqueness.
	if a == b {
		t.Fatal("two organizations got the same key")
	}
	orgPrefix := func(key string) string { return strings.SplitN(key, "/run/", 2)[0] }
	if orgPrefix(a) == orgPrefix(b) {
		t.Errorf("two organizations share the prefix %q", orgPrefix(a))
	}
}

func TestAnArtifactKeyRefusesWhatWouldLeaveItsPrefix(t *testing.T) {
	org, run := uuid.New(), uuid.New()

	for name, artifact := range map[string]string{
		"empty":                  "",
		"parent directory":       "../summary.json",
		"nested parent":          "a/../../summary.json",
		"a path":                 "a/summary.json",
		"absolute":               "/summary.json",
		"backslash":              `a\summary.json`,
		"a hidden file":          ".summary.json",
		"only dots":              "..",
		"dots inside":            "a..b",
		"a space":                "my summary.json",
		"a newline":              "summary.json\n",
		"a percent escape":       "%2e%2e",
		"a query":                "summary.json?versionId=1",
		"a fragment":             "summary.json#x",
		"non ascii":              "résumé.json",
		"a nul byte":             "summary\x00.json",
		"longer than the limit":  strings.Repeat("a", MaxArtifactNameLength+1),
		"a wildcard":             "*",
		"an uppercase extension": "summary.JSON",
	} {
		t.Run(name, func(t *testing.T) {
			if key, err := ArtifactKey(org, run, artifact); err == nil {
				t.Errorf("ArtifactKey accepted %q and returned %q", artifact, key)
			}
		})
	}
}

func TestAnArtifactKeyRefusesAnOrganizationOrRunThatIsNotThere(t *testing.T) {
	if _, err := ArtifactKey(uuid.Nil, uuid.New(), "summary.json"); err == nil {
		t.Error("a nil organization was accepted")
	}
	if _, err := ArtifactKey(uuid.New(), uuid.Nil, "summary.json"); err == nil {
		t.Error("a nil run was accepted")
	}
}

func TestAnArtifactKeyAcceptsTheNamesWeUse(t *testing.T) {
	for _, name := range []string{"summary.json", "k6-summary.json", "raw_output.json.gz", "run-1.har", "a"} {
		if _, err := ArtifactKey(uuid.New(), uuid.New(), name); err != nil {
			t.Errorf("ArtifactKey refused %q: %v", name, err)
		}
	}
}

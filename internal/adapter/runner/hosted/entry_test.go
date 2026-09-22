package hosted

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestBuildEntryScript(t *testing.T) {
	options := []byte(`{"scenarios":{"smoke":{"executor":"constant-vus","vus":1,"duration":"3s"}}}`)

	entry, err := buildEntryScript(options)
	if err != nil {
		t.Fatalf("buildEntryScript: %v", err)
	}

	// The entry module is what k6 runs; these are the exports it has to carry.
	for _, fragment := range []string{
		"import * as user from './user.js'",
		"export const options =",
		"export function setup()",
		"export function teardown(data)",
		"export default function (data)",
		"export function handleSummary(data)",
		`"/ballast/out/summary.json"`,
		`"executor":"constant-vus"`,
	} {
		if !strings.Contains(entry, fragment) {
			t.Errorf("the entry module is missing %q:\n%s", fragment, entry)
		}
	}
}

// renderedOptions pulls the options literal back out of the generated module.
func renderedOptions(t *testing.T, entry string) string {
	t.Helper()

	const prefix = "export const options = "
	const suffix = ";\n\nexport function setup()"

	start := strings.Index(entry, prefix)
	if start < 0 {
		t.Fatalf("the entry module declares no options:\n%s", entry)
	}
	start += len(prefix)

	end := strings.Index(entry[start:], suffix)
	if end < 0 {
		t.Fatalf("the options declaration is not terminated:\n%s", entry)
	}

	return entry[start : start+end]
}

// A threshold expression is text a user typed, and it travels into a generated
// JavaScript module. It is written there as JSON, so it can only ever be read
// as a string.
//
// Looking for the hostile text in the output would prove nothing: it is
// supposed to be there, inside its string literal. What matters is that the
// literal still closes where it should, which is what a round trip shows — had
// anything escaped, the remainder would no longer parse as the value that went
// in.
func TestBuildEntryScriptKeepsHostileTextInsideItsLiteral(t *testing.T) {
	const hostile = `p(95)<300"}}; import http from 'k6/http'; export const options = {vus: 9999}; //`

	options, err := json.Marshal(map[string]any{
		"thresholds": map[string][]string{"http_req_duration": {hostile}},
	})
	if err != nil {
		t.Fatalf("build the options: %v", err)
	}

	entry, err := buildEntryScript(options)
	if err != nil {
		t.Fatalf("buildEntryScript: %v", err)
	}

	var back struct {
		Thresholds map[string][]string `json:"thresholds"`
	}
	if err := json.Unmarshal([]byte(renderedOptions(t, entry)), &back); err != nil {
		t.Fatalf("the rendered options no longer parse, so something escaped: %v", err)
	}

	got := back.Thresholds["http_req_duration"]
	if len(got) != 1 || got[0] != hostile {
		t.Errorf("the expression came back as %q, want %q", got, hostile)
	}

	// One options declaration, not two. Counted at the start of a line: the
	// hostile text contains that phrase too, inside its string literal, which
	// is exactly where it is harmless.
	declarations := 0
	for _, line := range strings.Split(entry, "\n") {
		if strings.HasPrefix(line, "export const options") {
			declarations++
		}
	}
	if declarations != 1 {
		t.Errorf("the module declares options %d times", declarations)
	}
}

func TestBuildEntryScriptRejectsInvalidOptions(t *testing.T) {
	if _, err := buildEntryScript([]byte(`{not json`)); !errors.Is(err, ErrInvalidJob) {
		t.Fatalf("got %v, want ErrInvalidJob", err)
	}
}

func TestJobValidation(t *testing.T) {
	script := []byte("export default function () {}")
	options := []byte(`{"scenarios":{}}`)

	cases := []struct {
		name    string
		job     Job
		wantErr bool
	}{
		{"complete", Job{RunID: uuid.New(), Script: script, Options: options}, false},
		{"no run id", Job{Script: script, Options: options}, true},
		{"no scenario", Job{RunID: uuid.New(), Options: options}, true},
		{"no options", Job{RunID: uuid.New(), Script: script}, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.job.validate()

			if c.wantErr && !errors.Is(err, ErrInvalidJob) {
				t.Fatalf("got %v, want ErrInvalidJob", err)
			}
			if !c.wantErr && err != nil {
				t.Fatalf("a complete job was refused: %v", err)
			}
		})
	}
}

func TestConfigDefaults(t *testing.T) {
	cfg := Config{}.withDefaults()

	switch {
	case cfg.Image != DefaultImage:
		t.Errorf("Image = %q, want %q", cfg.Image, DefaultImage)
	case cfg.CPUs <= 0:
		t.Error("CPUs was left unset, so the container would be unbounded")
	case cfg.MemoryMB <= 0:
		t.Error("MemoryMB was left unset, so the container would be unbounded")
	case cfg.PidsLimit <= 0:
		t.Error("PidsLimit was left unset, so a fork loop would be unbounded")
	case cfg.TTL <= 0:
		t.Error("TTL was left unset, so a run could last forever")
	}
}

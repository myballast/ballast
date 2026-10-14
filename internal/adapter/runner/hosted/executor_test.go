package hosted

import (
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/usecase/run"
)

// The boundary this ticket is about, checked in the one direction a compiler
// cannot: a field that is not copied is not an error, it is a generator running
// without it.
//
// Both tests fill the source completely and then insist that nothing on the
// other side was left at its zero value. A field added to either struct and
// forgotten in the translation fails here, in a unit test, rather than in a
// containerised one — or nowhere at all.

// unset is every exported field of value that is still at its zero value.
//
// Reflection rather than a field-by-field comparison on purpose: a comparison
// written by hand has to be extended when a field is added, which is exactly
// the thing being forgotten.
func unset(t *testing.T, value any, except ...string) []string {
	t.Helper()

	skipped := make(map[string]bool, len(except))
	for _, name := range except {
		skipped[name] = true
	}

	reflected := reflect.ValueOf(value)
	var empty []string
	for i := range reflected.NumField() {
		field := reflected.Type().Field(i)
		if !field.IsExported() || skipped[field.Name] {
			continue
		}
		if reflected.Field(i).IsZero() {
			empty = append(empty, field.Name)
		}
	}

	return empty
}

func TestEveryFieldOfAJobCrossesTheBoundary(t *testing.T) {
	asked := run.Job{
		RunID:        uuid.New(),
		Script:       []byte("export default function () {}"),
		Options:      []byte(`{"scenarios":{}}`),
		Env:          map[string]string{"BASE_URL": "https://staging.example.test"},
		AllowedHosts: []string{"staging.example.test:443"},
	}

	// The source itself first: a field added to run.Job and left out of this
	// test would make the check below vacuous.
	if empty := unset(t, asked); len(empty) > 0 {
		t.Fatalf("this test does not fill %v of run.Job, so it proves nothing about them", empty)
	}

	// Network is this package's own, and the service has no such notion: a job
	// that names none makes its own network and removes it afterwards.
	translated := jobFrom(asked)
	if empty := unset(t, translated, "Network"); len(empty) > 0 {
		t.Fatalf("%v did not cross from run.Job into hosted.Job", empty)
	}

	if translated.RunID != asked.RunID ||
		string(translated.Script) != string(asked.Script) ||
		string(translated.Options) != string(asked.Options) ||
		translated.Env["BASE_URL"] != asked.Env["BASE_URL"] ||
		len(translated.AllowedHosts) != 1 || translated.AllowedHosts[0] != asked.AllowedHosts[0] {
		t.Fatalf("the job crossed as %+v", translated)
	}
	if translated.Network != "" {
		t.Errorf("the translation invented a network: %q", translated.Network)
	}
}

func TestEveryFieldOfAnOutcomeCrossesTheBoundary(t *testing.T) {
	reported := Result{
		Summary:        []byte(`{"metrics":{}}`),
		ExitCode:       99,
		TimedOut:       true,
		EgressBytes:    4096,
		IngressBytes:   65536,
		EgressMeasured: true,
	}

	if empty := unset(t, reported); len(empty) > 0 {
		t.Fatalf("this test does not fill %v of hosted.Result, so it proves nothing about them", empty)
	}

	translated := outcomeFrom(reported)
	if empty := unset(t, translated); len(empty) > 0 {
		t.Fatalf("%v did not cross from hosted.Result into run.Outcome", empty)
	}

	if string(translated.Summary) != string(reported.Summary) ||
		translated.ExitCode != reported.ExitCode ||
		translated.TimedOut != reported.TimedOut {
		t.Fatalf("the outcome crossed as %+v", translated)
	}

	// The egress is a struct of its own on the far side, so the check above
	// would pass with it left empty.
	if empty := unset(t, translated.Egress); len(empty) > 0 {
		t.Fatalf("%v did not cross into run.Egress", empty)
	}
	if translated.Egress.SentBytes != reported.EgressBytes ||
		translated.Egress.ReceivedBytes != reported.IngressBytes ||
		translated.Egress.Measured != reported.EgressMeasured {
		t.Fatalf("the egress crossed as %+v", translated.Egress)
	}
}

package scenario

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

type fakeScenarios struct {
	byID     map[uuid.UUID]domain.Scenario
	created  *domain.Scenario
	versions []domain.ScenarioVersion
	renames  []string
}

func (f *fakeScenarios) Create(
	_ context.Context, newScenario domain.Scenario, _ domain.Identity, at time.Time,
) (domain.Scenario, error) {
	newScenario.CreatedAt, newScenario.UpdatedAt = at, at
	f.created = &newScenario
	f.byID[newScenario.ID] = newScenario
	return newScenario, nil
}

func (f *fakeScenarios) ByID(_ context.Context, _, id uuid.UUID) (domain.Scenario, error) {
	if found, ok := f.byID[id]; ok {
		return found, nil
	}
	return domain.Scenario{}, domain.ErrNotFound
}

func (f *fakeScenarios) InProject(_ context.Context, _, projectID uuid.UUID) ([]domain.Scenario, error) {
	var found []domain.Scenario
	for _, s := range f.byID {
		if s.ProjectID == projectID {
			found = append(found, s)
		}
	}
	return found, nil
}

func (f *fakeScenarios) Rename(
	_ context.Context, _, id uuid.UUID, name string, at time.Time,
) (domain.Scenario, error) {
	found, ok := f.byID[id]
	if !ok {
		return domain.Scenario{}, domain.ErrNotFound
	}
	found.Name, found.UpdatedAt = name, at
	f.byID[id] = found
	f.renames = append(f.renames, name)
	return found, nil
}

func (f *fakeScenarios) NewVersion(
	_ context.Context, _, id uuid.UUID, version domain.ScenarioVersion, _ domain.Identity, at time.Time,
) (domain.Scenario, error) {
	found, ok := f.byID[id]
	if !ok {
		return domain.Scenario{}, domain.ErrNotFound
	}

	f.versions = append(f.versions, version)

	found.Script = version.Script
	found.Version = version.Version
	found.DefaultProfile = version.DefaultProfile
	found.DefaultProfileParams = version.DefaultProfileParams
	found.UpdatedAt = at
	f.byID[id] = found

	return found, nil
}

func (f *fakeScenarios) Versions(_ context.Context, _, _ uuid.UUID) ([]domain.ScenarioVersion, error) {
	return f.versions, nil
}

func (f *fakeScenarios) Delete(_ context.Context, _, id uuid.UUID) error {
	delete(f.byID, id)
	return nil
}

type fakeThresholds struct {
	added   []domain.Threshold
	removed []uuid.UUID
}

func (f *fakeThresholds) OfScenario(context.Context, uuid.UUID, uuid.UUID) ([]domain.Threshold, error) {
	return f.added, nil
}

func (f *fakeThresholds) Add(
	_ context.Context, _, _ uuid.UUID, threshold domain.Threshold,
) (domain.Threshold, error) {
	f.added = append(f.added, threshold)
	return threshold, nil
}

func (f *fakeThresholds) Remove(_ context.Context, _, _, id uuid.UUID) error {
	f.removed = append(f.removed, id)
	return nil
}

// reportingInspector hands back whatever the test set up, so the service's
// behaviour is tested rather than the parser's. What the parser decides is tested
// in adapter/k6.
type reportingInspector struct{ report domain.ScriptReport }

func (i reportingInspector) Inspect(string) domain.ScriptReport { return i.report }

func refusing() domain.ScriptReport {
	return domain.ScriptReport{Findings: []domain.ScriptFinding{{
		Severity: domain.ScriptError,
		Code:     domain.ScriptHardcodedURL,
		Message:  "the script addresses a host of its own",
		Detail:   "https://somewhere.example.test/",
	}}}
}

func warning() domain.ScriptReport {
	return domain.ScriptReport{Findings: []domain.ScriptFinding{{
		Severity: domain.ScriptWarning,
		Code:     domain.ScriptNoRequestNameTags,
		Message:  "the script makes 3 requests and tags none of them with a name",
	}}}
}

type allowAll struct{}

func (allowAll) Can(context.Context, domain.Identity, domain.Permission, uuid.UUID) (bool, error) {
	return true, nil
}

type refuseAll struct{}

func (refuseAll) Can(context.Context, domain.Identity, domain.Permission, uuid.UUID) (bool, error) {
	return false, nil
}

type fixedClock struct{ at time.Time }

func (c fixedClock) Now() time.Time { return c.at }

type fixture struct {
	service    *Service
	scenarios  *fakeScenarios
	thresholds *fakeThresholds
	who        domain.Identity
	projectID  uuid.UUID
}

func newFixture(report domain.ScriptReport, authz Authorizer) fixture {
	scenarios := &fakeScenarios{byID: map[uuid.UUID]domain.Scenario{}}
	thresholds := &fakeThresholds{}

	return fixture{
		service: NewService(
			scenarios, thresholds,
			reportingInspector{report: report},
			authz,
			fixedClock{at: time.Unix(0, 0).UTC()},
			slog.New(slog.NewTextHandler(io.Discard, nil)),
		),
		scenarios:  scenarios,
		thresholds: thresholds,
		who:        domain.Identity{UserID: uuid.New(), OrgID: uuid.New()},
		projectID:  uuid.New(),
	}
}

func (f fixture) request() NewScenario {
	return NewScenario{
		ProjectID: f.projectID,
		Name:      "checkout",
		Script:    "export default function () {}",
	}
}

func TestCreateStoresTheFirstVersion(t *testing.T) {
	f := newFixture(domain.ScriptReport{}, allowAll{})

	created, report, err := f.service.Create(context.Background(), f.who, f.request())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	switch {
	case created.Version != 1:
		t.Errorf("a new scenario is at version %d, want 1", created.Version)
	case created.ProjectID != f.projectID:
		t.Error("the scenario was not attached to the project")
	case created.DefaultProfile != domain.ProfileSmoke:
		t.Errorf("default profile is %q, want smoke", created.DefaultProfile)
	case len(report.Findings) != 0:
		t.Errorf("findings: %+v", report.Findings)
	}

	// With no parameters given, the profile's own template is stored rather than
	// zeros, which would describe no runnable test.
	template, _ := domain.ProfileSmoke.Defaults()
	if created.DefaultProfileParams != template {
		t.Errorf("default params are %+v, want the smoke template %+v",
			created.DefaultProfileParams, template)
	}
}

// A script that would reach a host of its own is refused rather than stored and
// refused later.
func TestCreateRefusesABadScript(t *testing.T) {
	f := newFixture(refusing(), allowAll{})

	_, report, err := f.service.Create(context.Background(), f.who, f.request())
	if !errors.Is(err, domain.ErrInvalidScript) {
		t.Fatalf("got %v, want ErrInvalidScript", err)
	}
	if f.scenarios.created != nil {
		t.Error("a refused script was stored")
	}

	// The findings come back, so the caller is told what to fix.
	if len(report.Errors()) == 0 {
		t.Error("the refusal carried no findings")
	}
}

// The second acceptance criterion: flagged, not refused.
func TestAScriptWithoutNameTagsIsFlaggedAndStored(t *testing.T) {
	f := newFixture(warning(), allowAll{})

	created, report, err := f.service.Create(context.Background(), f.who, f.request())
	if err != nil {
		t.Fatalf("a warning refused the import: %v", err)
	}
	if created.ID == uuid.Nil {
		t.Fatal("nothing was stored")
	}

	if len(report.Warnings()) != 1 {
		t.Fatalf("warnings: %+v", report.Findings)
	}
	if report.Warnings()[0].Code != domain.ScriptNoRequestNameTags {
		t.Errorf("code: %q", report.Warnings()[0].Code)
	}
}

// And the warning is still there when the scenario is read, which is where
// somebody about to launch a run sees it.
func TestReadingAScenarioCarriesItsWarnings(t *testing.T) {
	f := newFixture(warning(), allowAll{})

	created, _, err := f.service.Create(context.Background(), f.who, f.request())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, report, err := f.service.ByID(context.Background(), f.who, f.projectID, created.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if len(report.Warnings()) != 1 {
		t.Errorf("reading the scenario lost its warnings: %+v", report.Findings)
	}
}

// Changing the script writes a version. Nothing overwrites the one before it.
func TestChangingTheScriptWritesAVersion(t *testing.T) {
	f := newFixture(domain.ScriptReport{}, allowAll{})

	created, _, err := f.service.Create(context.Background(), f.who, f.request())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	next := "export default function () { /* changed */ }"
	updated, _, err := f.service.Update(context.Background(), f.who, f.projectID, created.ID, Update{
		Script: &next,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if updated.Version != 2 {
		t.Errorf("version is %d, want 2", updated.Version)
	}
	if len(f.scenarios.versions) != 1 {
		t.Fatalf("%d version(s) were written", len(f.scenarios.versions))
	}
	if f.scenarios.versions[0].Script != next {
		t.Error("the version does not hold the new script")
	}
	if f.scenarios.versions[0].Version != 2 {
		t.Errorf("the version is numbered %d", f.scenarios.versions[0].Version)
	}
}

// A name is not something a run executed, so renaming writes none.
func TestRenamingWritesNoVersion(t *testing.T) {
	f := newFixture(domain.ScriptReport{}, allowAll{})

	created, _, err := f.service.Create(context.Background(), f.who, f.request())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	name := "checkout and payments"
	updated, _, err := f.service.Update(context.Background(), f.who, f.projectID, created.ID, Update{
		Name: &name,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if updated.Name != name {
		t.Errorf("name is %q", updated.Name)
	}
	if updated.Version != 1 {
		t.Errorf("renaming moved the version to %d", updated.Version)
	}
	if len(f.scenarios.versions) != 0 {
		t.Errorf("renaming wrote %d version(s)", len(f.scenarios.versions))
	}
}

// Submitting the same script again writes nothing: a version per save would make
// the history unreadable.
func TestResubmittingTheSameScriptWritesNoVersion(t *testing.T) {
	f := newFixture(domain.ScriptReport{}, allowAll{})

	created, _, err := f.service.Create(context.Background(), f.who, f.request())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	same := created.Script
	updated, _, err := f.service.Update(context.Background(), f.who, f.projectID, created.ID, Update{
		Script: &same,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if updated.Version != 1 {
		t.Errorf("version moved to %d for an unchanged script", updated.Version)
	}
	if len(f.scenarios.versions) != 0 {
		t.Errorf("%d version(s) written for an unchanged script", len(f.scenarios.versions))
	}
}

func TestUpdateRefusesABadScript(t *testing.T) {
	f := newFixture(domain.ScriptReport{}, allowAll{})

	created, _, err := f.service.Create(context.Background(), f.who, f.request())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// From here on the inspector refuses.
	f.service.inspector = reportingInspector{report: refusing()}

	bad := "export default function () { fetch('https://somewhere.example.test/'); }"
	if _, _, err := f.service.Update(context.Background(), f.who, f.projectID, created.ID, Update{
		Script: &bad,
	}); !errors.Is(err, domain.ErrInvalidScript) {
		t.Fatalf("got %v, want ErrInvalidScript", err)
	}
	if len(f.scenarios.versions) != 0 {
		t.Error("a refused script was written as a version")
	}
}

// A scenario of another project reached through a project the caller may see
// answers as absent. Row-level security has nothing to say: both rows are the
// caller's organization.
func TestAScenarioOfAnotherProjectIsNotFound(t *testing.T) {
	f := newFixture(domain.ScriptReport{}, allowAll{})

	elsewhere := f.request()
	elsewhere.ProjectID = uuid.New()

	created, _, err := f.service.Create(context.Background(), f.who, elsewhere)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, _, err := f.service.ByID(context.Background(), f.who, f.projectID, created.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestEveryOperationIsGuarded(t *testing.T) {
	f := newFixture(domain.ScriptReport{}, refuseAll{})
	id := uuid.New()

	operations := map[string]func() error{
		"list":   func() error { _, err := f.service.InProject(context.Background(), f.who, f.projectID); return err },
		"create": func() error { _, _, err := f.service.Create(context.Background(), f.who, f.request()); return err },
		"read":   func() error { _, _, err := f.service.ByID(context.Background(), f.who, f.projectID, id); return err },
		"update": func() error {
			_, _, err := f.service.Update(context.Background(), f.who, f.projectID, id, Update{})
			return err
		},
		"delete":   func() error { return f.service.Delete(context.Background(), f.who, f.projectID, id) },
		"versions": func() error { _, err := f.service.Versions(context.Background(), f.who, f.projectID, id); return err },
		"thresholds": func() error {
			_, err := f.service.Thresholds(context.Background(), f.who, f.projectID, id)
			return err
		},
		"add threshold": func() error {
			_, err := f.service.AddThreshold(context.Background(), f.who, f.projectID, id, "http_req_failed", "rate<0.01")
			return err
		},
		"remove threshold": func() error {
			return f.service.RemoveThreshold(context.Background(), f.who, f.projectID, id, uuid.New())
		},
	}

	for name, call := range operations {
		t.Run(name, func(t *testing.T) {
			if err := call(); !errors.Is(err, domain.ErrForbidden) {
				t.Fatalf("got %v, want ErrForbidden", err)
			}
		})
	}

	if f.scenarios.created != nil || len(f.thresholds.added) > 0 {
		t.Error("something was written despite the refusals")
	}
}

func TestThresholdsAreValidated(t *testing.T) {
	f := newFixture(domain.ScriptReport{}, allowAll{})

	created, _, err := f.service.Create(context.Background(), f.who, f.request())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	for _, c := range []struct{ metric, expression string }{
		{"", "rate<0.01"},
		{"http_req_failed", ""},
		{"   ", "rate<0.01"},
	} {
		if _, err := f.service.AddThreshold(
			context.Background(), f.who, f.projectID, created.ID, c.metric, c.expression,
		); !errors.Is(err, domain.ErrInvalidThreshold) {
			t.Errorf("metric=%q expression=%q: got %v", c.metric, c.expression, err)
		}
	}
	if len(f.thresholds.added) > 0 {
		t.Error("an invalid rule was written")
	}

	// The tagged syntax is accepted untouched: it is k6 that parses it.
	tagged := "http_req_duration{name:POST /login}"
	added, err := f.service.AddThreshold(context.Background(), f.who, f.projectID, created.ID, tagged, "p(95)<300")
	if err != nil {
		t.Fatalf("a tagged metric was refused: %v", err)
	}
	if added.Metric != tagged {
		t.Errorf("the metric was altered to %q", added.Metric)
	}
}

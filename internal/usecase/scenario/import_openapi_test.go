package scenario

import (
	"context"
	"errors"
	"testing"

	"github.com/youssefmahersi/ballast/internal/domain"
)

func shopSpecification() domain.ImportedSpec {
	return domain.ImportedSpec{
		Title: "Shop",
		Endpoints: []domain.ImportedEndpoint{
			{Method: "GET", Path: "/users/{id}", Tag: "users"},
			{Method: "POST", Path: "/orders", Tag: "orders", ContentType: "application/json", Body: `{"item":"thing"}`},
			{Method: "DELETE", Path: "/orders/{id}", Tag: "orders"},
		},
	}
}

// importFixture is a fixture whose reader returns the shop specification.
func importFixture(authz Authorizer) (fixture, *fakeSpecReader) {
	f := newFixture(domain.ScriptReport{}, authz)

	reader := &fakeSpecReader{spec: shopSpecification()}
	f.service.specs = reader

	return f, reader
}

func (f fixture) importRequest(references ...domain.EndpointRef) OpenAPIImport {
	return OpenAPIImport{
		ProjectID: f.projectID,
		Name:      "shop endpoints",
		Spec:      []byte("the file"),
		Endpoints: references,
	}
}

func TestPreviewListsTheEndpointsOfTheFile(t *testing.T) {
	f, _ := importFixture(allowAll{})

	spec, err := f.service.PreviewOpenAPI(context.Background(), f.who, f.projectID, []byte("the file"))
	if err != nil {
		t.Fatalf("PreviewOpenAPI: %v", err)
	}
	if len(spec.Endpoints) != 3 {
		t.Errorf("%d endpoints, wanted 3", len(spec.Endpoints))
	}
	if f.scenarios.created != nil {
		t.Error("a preview stored a scenario")
	}
}

func TestPreviewReadsNothingForSomebodyWhoCannotImport(t *testing.T) {
	f, reader := importFixture(refuseAll{})

	_, err := f.service.PreviewOpenAPI(context.Background(), f.who, f.projectID, []byte("the file"))
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("error = %v, wanted ErrForbidden", err)
	}
	if reader.calls != 0 {
		t.Error("the file was read for somebody who may not import")
	}
}

func TestImportStoresAGraphScenarioInTheOrderChosen(t *testing.T) {
	f, _ := importFixture(allowAll{})

	created, _, notes, err := f.service.ImportOpenAPI(context.Background(), f.who, f.importRequest(
		domain.EndpointRef{Method: "POST", Path: "/orders"},
		domain.EndpointRef{Method: "GET", Path: "/users/{id}"},
	))
	if err != nil {
		t.Fatalf("ImportOpenAPI: %v", err)
	}
	if len(notes) != 0 {
		t.Errorf("notes = %v, wanted none", notes)
	}

	if created.Mode != domain.ModeGraph || created.Graph == nil {
		t.Fatalf("mode = %q, graph = %v, wanted a graph scenario", created.Mode, created.Graph)
	}
	if created.Script != "// generated" {
		t.Errorf("script = %q, wanted the generated one", created.Script)
	}
	if created.Version != 1 || created.ProjectID != f.projectID || created.Name != "shop endpoints" {
		t.Errorf("scenario = %+v", created)
	}

	nodes := created.Graph.Nodes
	if len(nodes) != 3 || nodes[0].Path != "/orders" || nodes[1].Path != "/users/{id}" || nodes[2].Kind != domain.NodeEnd {
		t.Errorf("nodes = %+v, wanted /orders, /users/{id}, then the end", nodes)
	}
	if nodes[0].Body != `{"item":"thing"}` {
		t.Errorf("the example body was not carried over: %q", nodes[0].Body)
	}
}

func TestImportKeepsAnEndpointChosenTwiceOnce(t *testing.T) {
	f, _ := importFixture(allowAll{})

	created, _, _, err := f.service.ImportOpenAPI(context.Background(), f.who, f.importRequest(
		domain.EndpointRef{Method: "GET", Path: "/users/{id}"},
		domain.EndpointRef{Method: "GET", Path: "/users/{id}"},
	))
	if err != nil {
		t.Fatalf("ImportOpenAPI: %v", err)
	}
	if len(created.Graph.Nodes) != 2 {
		t.Errorf("%d nodes, wanted one request and the end", len(created.Graph.Nodes))
	}
}

func TestImportReadsAndStoresNothingForSomebodyWhoCannotCreateScenarios(t *testing.T) {
	f, reader := importFixture(refuseAll{})

	_, _, _, err := f.service.ImportOpenAPI(context.Background(), f.who, f.importRequest(
		domain.EndpointRef{Method: "GET", Path: "/users/{id}"},
	))
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("error = %v, wanted ErrForbidden", err)
	}
	if reader.calls != 0 || f.scenarios.created != nil {
		t.Errorf("read %d times, stored %v", reader.calls, f.scenarios.created)
	}
}

func TestImportRefusesAnEndpointTheFileDoesNotHave(t *testing.T) {
	f, _ := importFixture(allowAll{})

	_, _, _, err := f.service.ImportOpenAPI(context.Background(), f.who, f.importRequest(
		domain.EndpointRef{Method: "GET", Path: "/users/{id}"},
		domain.EndpointRef{Method: "GET", Path: "/nothing"},
	))
	if !errors.Is(err, domain.ErrInvalidSpec) {
		t.Fatalf("error = %v, wanted ErrInvalidSpec", err)
	}
	if f.scenarios.created != nil {
		t.Error("a scenario was stored for a choice that included an unknown endpoint")
	}
}

func TestImportRefusesAChoiceOfNothing(t *testing.T) {
	f, _ := importFixture(allowAll{})

	_, _, _, err := f.service.ImportOpenAPI(context.Background(), f.who, f.importRequest())
	if !errors.Is(err, domain.ErrInvalidSpec) {
		t.Fatalf("error = %v, wanted ErrInvalidSpec", err)
	}
}

func TestImportPassesOnWhatTheReaderRefused(t *testing.T) {
	f, reader := importFixture(allowAll{})
	reader.err = domain.ErrInvalidSpec

	_, _, _, err := f.service.ImportOpenAPI(context.Background(), f.who, f.importRequest(
		domain.EndpointRef{Method: "GET", Path: "/users/{id}"},
	))
	if !errors.Is(err, domain.ErrInvalidSpec) {
		t.Fatalf("error = %v, wanted ErrInvalidSpec", err)
	}
	if f.scenarios.created != nil {
		t.Error("a scenario was stored from a file the reader refused")
	}
}

func TestImportReportsWhatCouldNotBeCarriedOver(t *testing.T) {
	f, reader := importFixture(allowAll{})
	reader.spec.Endpoints[1].BodyTooLong = true

	_, _, notes, err := f.service.ImportOpenAPI(context.Background(), f.who, f.importRequest(
		domain.EndpointRef{Method: "POST", Path: "/orders"},
	))
	if err != nil {
		t.Fatalf("ImportOpenAPI: %v", err)
	}
	if len(notes) != 1 {
		t.Errorf("notes = %v, wanted one", notes)
	}
}

func TestImportStillInspectsTheGeneratedScript(t *testing.T) {
	f := newFixture(refusing(), allowAll{})
	f.service.specs = &fakeSpecReader{spec: shopSpecification()}

	_, report, _, err := f.service.ImportOpenAPI(context.Background(), f.who, f.importRequest(
		domain.EndpointRef{Method: "GET", Path: "/users/{id}"},
	))
	if !errors.Is(err, domain.ErrInvalidScript) {
		t.Fatalf("error = %v, wanted ErrInvalidScript", err)
	}
	if len(report.Errors()) == 0 || f.scenarios.created != nil {
		t.Errorf("findings = %d, stored = %v", len(report.Errors()), f.scenarios.created)
	}
}

func TestCreateRefusesAGraphThatDoesNotValidate(t *testing.T) {
	f := newFixture(domain.ScriptReport{}, allowAll{})
	request := f.request()
	request.Graph = &domain.ScenarioGraph{Start: "nowhere"}

	_, _, err := f.service.Create(context.Background(), f.who, request)
	if !errors.Is(err, domain.ErrInvalidGraph) {
		t.Fatalf("error = %v, wanted ErrInvalidGraph", err)
	}
	if f.scenarios.created != nil {
		t.Error("a graph that does not validate was stored")
	}
}

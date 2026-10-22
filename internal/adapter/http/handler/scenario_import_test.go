package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/adapter/http/dto"
	"github.com/youssefmahersi/ballast/internal/adapter/http/handler"
	"github.com/youssefmahersi/ballast/internal/adapter/http/session"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/scenario"
)

// importScenarios stores what the service hands it and nothing else.
type importScenarios struct{ created []domain.Scenario }

func (s *importScenarios) Create(
	_ context.Context, newScenario domain.Scenario, _ domain.Identity, at time.Time,
) (domain.Scenario, error) {
	newScenario.CreatedAt, newScenario.UpdatedAt = at, at
	s.created = append(s.created, newScenario)
	return newScenario, nil
}

func (*importScenarios) ByID(context.Context, uuid.UUID, uuid.UUID) (domain.Scenario, error) {
	return domain.Scenario{}, domain.ErrNotFound
}

func (*importScenarios) InProject(context.Context, uuid.UUID, uuid.UUID) ([]domain.Scenario, error) {
	return nil, nil
}

func (*importScenarios) Rename(context.Context, uuid.UUID, uuid.UUID, string, time.Time) (domain.Scenario, error) {
	return domain.Scenario{}, domain.ErrNotFound
}

func (*importScenarios) NewVersion(
	context.Context, uuid.UUID, uuid.UUID, domain.ScenarioVersion, domain.Identity, time.Time,
) (domain.Scenario, error) {
	return domain.Scenario{}, domain.ErrNotFound
}

func (*importScenarios) Versions(context.Context, uuid.UUID, uuid.UUID) ([]domain.ScenarioVersion, error) {
	return nil, nil
}

func (*importScenarios) Delete(context.Context, uuid.UUID, uuid.UUID) error { return nil }

type noThresholds struct{ scenario.Thresholds }

type acceptingInspector struct{}

func (acceptingInspector) Inspect(string) domain.ScriptReport { return domain.ScriptReport{} }

// importReader returns a fixed specification, or refuses with a fixed error.
type importReader struct {
	spec domain.ImportedSpec
	err  error
}

func (r importReader) Read([]byte) (domain.ImportedSpec, error) { return r.spec, r.err }

// importedCollections returns an empty collection: these tests are about
// specifications.
type importedCollections struct{}

func (importedCollections) Read(_, _ []byte) (domain.ImportedCollection, error) {
	return domain.ImportedCollection{}, nil
}

type importGenerator struct{}

func (importGenerator) Generate(domain.ScenarioGraph) (string, error) { return "// generated", nil }

type everyone struct{}

func (everyone) Can(context.Context, domain.Identity, domain.Permission, uuid.UUID) (bool, error) {
	return true, nil
}

type fixedNow struct{}

func (fixedNow) Now() time.Time { return time.Unix(0, 0).UTC() }

func importHandler(reader importReader) (*handler.Scenario, *importScenarios) {
	store := &importScenarios{}
	service := scenario.NewService(
		store, noThresholds{}, acceptingInspector{}, reader, scenario.CollectionReaders{Postman: importedCollections{}}, importGenerator{}, everyone{}, fixedNow{}, discard())
	return handler.NewScenario(service, discard()), store
}

func importRequest(body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	request.SetPathValue("projectId", uuid.NewString())

	who := domain.Identity{UserID: uuid.New(), OrgID: uuid.New()}
	return request.WithContext(session.WithIdentity(request.Context(), who))
}

func shop() domain.ImportedSpec {
	return domain.ImportedSpec{
		Title:    "Shop",
		BasePath: "/v1",
		Endpoints: []domain.ImportedEndpoint{{
			Method: "GET", Path: "/users/{id}", Tag: "users", PathParams: []string{"id"},
		}, {
			Method: "POST", Path: "/orders", ContentType: "application/json", Body: `{}`,
		}},
		Schemes: []domain.AuthScheme{{Name: "key", Type: "apiKey", Detail: "header X-API-Key"}},
	}
}

func TestThePreviewListsEndpointsSchemesAndTheBasePath(t *testing.T) {
	h, _ := importHandler(importReader{spec: shop()})

	recorder := httptest.NewRecorder()
	h.PreviewOpenAPI(recorder, importRequest(`{"spec":"the file"}`))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body)
	}

	var response dto.OpenAPIPreviewResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if response.Title != "Shop" || response.BasePath != "/v1" || len(response.Endpoints) != 2 {
		t.Errorf("response = %+v", response)
	}
	if !response.Endpoints[1].HasBody || response.Endpoints[0].HasBody {
		t.Errorf("has_body = %v and %v, wanted false then true", response.Endpoints[0].HasBody, response.Endpoints[1].HasBody)
	}
	if len(response.Schemes) != 1 || response.Schemes[0].Detail != "header X-API-Key" {
		t.Errorf("schemes = %+v", response.Schemes)
	}
	if response.Notes == nil {
		t.Error("notes should be an empty list, not null")
	}
}

func TestAnImportCreatesTheScenarioAndAnswers201(t *testing.T) {
	h, store := importHandler(importReader{spec: shop()})

	recorder := httptest.NewRecorder()
	h.ImportOpenAPI(recorder, importRequest(
		`{"name":"shop","spec":"the file","endpoints":[{"method":"GET","path":"/users/{id}"}]}`))

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body)
	}

	var response dto.ImportOpenAPIResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if response.Scenario.Mode != "graph" || response.Scenario.Script != "// generated" {
		t.Errorf("scenario = %+v", response.Scenario)
	}
	if response.Notes == nil {
		t.Error("notes should be an empty list, not null")
	}
	if len(store.created) != 1 {
		t.Errorf("%d scenarios stored, wanted 1", len(store.created))
	}
}

func TestARefusedFileAnswers422WithTheReason(t *testing.T) {
	h, store := importHandler(importReader{err: domain.ErrInvalidSpec})

	recorder := httptest.NewRecorder()
	h.ImportOpenAPI(recorder, importRequest(
		`{"name":"shop","spec":"the file","endpoints":[{"method":"GET","path":"/a"}]}`))

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body)
	}
	if len(store.created) != 0 {
		t.Error("a scenario was stored from a refused file")
	}
}

func TestAnEndpointTheFileDoesNotHaveAnswers422(t *testing.T) {
	h, _ := importHandler(importReader{spec: shop()})

	recorder := httptest.NewRecorder()
	h.ImportOpenAPI(recorder, importRequest(
		`{"name":"shop","spec":"the file","endpoints":[{"method":"GET","path":"/nothing"}]}`))

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body)
	}
}

func TestABodyThatIsNotJSONAnswers400(t *testing.T) {
	h, _ := importHandler(importReader{spec: shop()})

	for name, call := range map[string]http.HandlerFunc{"preview": h.PreviewOpenAPI, "import": h.ImportOpenAPI} {
		recorder := httptest.NewRecorder()
		call(recorder, importRequest(`not json`))

		if recorder.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, wanted 400", name, recorder.Code)
		}
	}
}

func TestARequestPastTheLimitAnswers413(t *testing.T) {
	h, _ := importHandler(importReader{spec: shop()})

	tooBig := `{"spec":"` + strings.Repeat("a", 2*domain.MaxSpecBytes+2*1024*1024) + `"}`

	for name, call := range map[string]http.HandlerFunc{"preview": h.PreviewOpenAPI, "import": h.ImportOpenAPI} {
		recorder := httptest.NewRecorder()
		call(recorder, importRequest(tooBig))

		if recorder.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("%s: status = %d, wanted 413", name, recorder.Code)
		}
	}
}

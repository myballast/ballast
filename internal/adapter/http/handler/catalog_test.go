package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/adapter/http/dto"
	"github.com/youssefmahersi/ballast/internal/adapter/http/handler"
	"github.com/youssefmahersi/ballast/internal/adapter/http/session"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/catalog"
)

// catalogStore keeps what the service writes, and answers the list with it.
type catalogStore struct {
	endpoints []domain.CatalogEndpoint
	filter    domain.CatalogFilter
	missing   bool
}

func (s *catalogStore) ReplaceSource(
	_ context.Context, _, _ uuid.UUID, _ string, endpoints []domain.CatalogEndpoint,
) error {
	s.endpoints = endpoints
	return nil
}

func (s *catalogStore) List(
	_ context.Context, _, _ uuid.UUID, filter domain.CatalogFilter,
) ([]domain.CatalogEndpoint, error) {
	s.filter = filter
	return s.endpoints, nil
}

func (s *catalogStore) Delete(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error {
	if s.missing {
		return domain.ErrNotFound
	}
	return nil
}

func (*catalogStore) DeleteSource(context.Context, uuid.UUID, uuid.UUID, string) (int, error) {
	return 4, nil
}

type nobody struct{}

func (nobody) Can(context.Context, domain.Identity, domain.Permission, uuid.UUID) (bool, error) {
	return false, nil
}

func catalogHandler(authz catalog.Authorizer) (*handler.Catalog, *catalogStore) {
	store := &catalogStore{}
	service := catalog.NewService(store, importReader{spec: shop()}, importedCollections{}, authz, fixedNow{}, discard())
	return handler.NewCatalog(service, discard()), store
}

func catalogRequest(target, body string, pathValues map[string]string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	request.SetPathValue("projectId", uuid.NewString())
	for name, value := range pathValues {
		request.SetPathValue(name, value)
	}

	who := domain.Identity{UserID: uuid.New(), OrgID: uuid.New()}
	return request.WithContext(session.WithIdentity(request.Context(), who))
}

func TestImportingToTheCatalogAnswers201WithTheCount(t *testing.T) {
	h, store := catalogHandler(everyone{})

	recorder := httptest.NewRecorder()
	h.ImportOpenAPI(recorder, catalogRequest("/", `{"source":"shop.yaml","spec":"x","endpoints":[{"method":"GET","path":"/users/{id}"},{"method":"POST","path":"/orders"}]}`, nil))

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body)
	}
	var response dto.CatalogImportResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil || response.Count != 2 {
		t.Errorf("response = %+v, %v", response, err)
	}
	if len(store.endpoints) != 2 {
		t.Errorf("%d endpoints stored", len(store.endpoints))
	}
}

func TestTheCatalogAnswersForbiddenToSomebodyWhoMayNotManageScenarios(t *testing.T) {
	h, store := catalogHandler(nobody{})

	recorder := httptest.NewRecorder()
	h.ImportOpenAPI(recorder, catalogRequest("/", `{"source":"s","spec":"x","endpoints":[{"method":"GET","path":"/users/{id}"}]}`, nil))

	if recorder.Code != http.StatusForbidden || len(store.endpoints) != 0 {
		t.Errorf("status = %d, stored = %d", recorder.Code, len(store.endpoints))
	}
}

func TestTheCatalogRefusalsHaveTheirOwnStatuses(t *testing.T) {
	cases := map[string]struct {
		body   string
		status int
	}{
		"not json":            {`nope`, http.StatusBadRequest},
		"no source name":      {`{"source":" ","spec":"x","endpoints":[{"method":"GET","path":"/users/{id}"}]}`, http.StatusBadRequest},
		"an empty choice":     {`{"source":"s","spec":"x","endpoints":[]}`, http.StatusUnprocessableEntity},
		"an unknown endpoint": {`{"source":"s","spec":"x","endpoints":[{"method":"GET","path":"/nothing"}]}`, http.StatusUnprocessableEntity},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			h, store := catalogHandler(everyone{})

			recorder := httptest.NewRecorder()
			h.ImportOpenAPI(recorder, catalogRequest("/", c.body, nil))

			if recorder.Code != c.status || len(store.endpoints) != 0 {
				t.Errorf("status = %d (wanted %d), stored = %d", recorder.Code, c.status, len(store.endpoints))
			}
		})
	}
}

func TestListingPassesTheFilterAndAnswersAnEmptyListNotNull(t *testing.T) {
	h, store := catalogHandler(everyone{})

	recorder := httptest.NewRecorder()
	h.List(recorder, catalogRequest("/?q=user&tag=users&source=shop.yaml", "", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	if store.filter != (domain.CatalogFilter{Query: "user", Tag: "users", Source: "shop.yaml"}) {
		t.Errorf("filter = %+v", store.filter)
	}
	if !strings.Contains(recorder.Body.String(), `"endpoints":[]`) {
		t.Errorf("body = %s, wanted an empty list", recorder.Body)
	}
}

func TestDeletingAnEndpoint(t *testing.T) {
	h, store := catalogHandler(everyone{})

	recorder := httptest.NewRecorder()
	h.Delete(recorder, catalogRequest("/", "", map[string]string{"endpointId": "not-a-uuid"}))
	if recorder.Code != http.StatusBadRequest {
		t.Errorf("a malformed id answered %d", recorder.Code)
	}

	recorder = httptest.NewRecorder()
	h.Delete(recorder, catalogRequest("/", "", map[string]string{"endpointId": uuid.NewString()}))
	if recorder.Code != http.StatusNoContent {
		t.Errorf("deleting answered %d", recorder.Code)
	}

	store.missing = true
	recorder = httptest.NewRecorder()
	h.Delete(recorder, catalogRequest("/", "", map[string]string{"endpointId": uuid.NewString()}))
	if recorder.Code != http.StatusNotFound {
		t.Errorf("deleting what is not there answered %d", recorder.Code)
	}
}

func TestDeletingASourceNeedsItsNameAndSaysHowManyWereRemoved(t *testing.T) {
	h, _ := catalogHandler(everyone{})

	recorder := httptest.NewRecorder()
	h.DeleteSource(recorder, catalogRequest("/", "", nil))
	if recorder.Code != http.StatusBadRequest {
		t.Errorf("no source answered %d", recorder.Code)
	}

	recorder = httptest.NewRecorder()
	h.DeleteSource(recorder, catalogRequest("/?source=shop.yaml", "", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"removed":4`) {
		t.Errorf("status = %d, body = %s", recorder.Code, recorder.Body)
	}
}

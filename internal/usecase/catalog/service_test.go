package catalog

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

type fakeRepository struct {
	replaced      []domain.CatalogEndpoint
	replacedSrc   string
	replaceCalls  int
	deletedSource string
	deletedID     uuid.UUID
	listFilter    domain.CatalogFilter
}

func (f *fakeRepository) ReplaceSource(
	_ context.Context, _, _ uuid.UUID, source string, endpoints []domain.CatalogEndpoint,
) error {
	f.replaceCalls++
	f.replacedSrc = source
	f.replaced = endpoints
	return nil
}

func (f *fakeRepository) List(
	_ context.Context, _, _ uuid.UUID, filter domain.CatalogFilter,
) ([]domain.CatalogEndpoint, error) {
	f.listFilter = filter
	return f.replaced, nil
}

func (f *fakeRepository) Delete(_ context.Context, _, _, id uuid.UUID) error {
	f.deletedID = id
	return nil
}

func (f *fakeRepository) DeleteSource(_ context.Context, _, _ uuid.UUID, source string) (int, error) {
	f.deletedSource = source
	return 3, nil
}

type fakeReader struct {
	spec  domain.ImportedSpec
	err   error
	calls int
}

func (f *fakeReader) Read([]byte) (domain.ImportedSpec, error) {
	f.calls++
	return f.spec, f.err
}

type fakeCollections struct {
	collection domain.ImportedCollection
	calls      int
}

func (f *fakeCollections) Read(_, _ []byte) (domain.ImportedCollection, error) {
	f.calls++
	return f.collection, nil
}

type decision bool

func (d decision) Can(context.Context, domain.Identity, domain.Permission, uuid.UUID) (bool, error) {
	return bool(d), nil
}

type fixedClock struct{}

func (fixedClock) Now() time.Time { return time.Unix(100, 0).UTC() }

type fixture struct {
	service    *Service
	repository *fakeRepository
	reader     *fakeReader
	who        domain.Identity
	projectID  uuid.UUID
}

func newFixture(allowed bool) fixture {
	repository := &fakeRepository{}
	reader := &fakeReader{spec: domain.ImportedSpec{Endpoints: []domain.ImportedEndpoint{
		{Method: "GET", Path: "/users/{id}", Tag: "users"},
		{Method: "POST", Path: "/orders", Tag: "orders", ContentType: "application/json", Body: `{}`},
		{Method: "DELETE", Path: "/orders/{id}", Tag: "orders"},
	}}}

	return fixture{
		service: NewService(repository, reader, CollectionReaders{Postman: &fakeCollections{}}, decision(allowed), fixedClock{},
			slog.New(slog.NewTextHandler(io.Discard, nil))),
		repository: repository,
		reader:     reader,
		who:        domain.Identity{UserID: uuid.New(), OrgID: uuid.New()},
		projectID:  uuid.New(),
	}
}

func (f fixture) request(references ...domain.EndpointRef) Import {
	return Import{ProjectID: f.projectID, Source: "shop.yaml", Spec: []byte("the file"), Endpoints: references}
}

func TestImportReplacesTheSourceWithTheChosenEndpoints(t *testing.T) {
	f := newFixture(true)

	count, err := f.service.Import(context.Background(), f.who, f.request(
		domain.EndpointRef{Method: "POST", Path: "/orders"},
		domain.EndpointRef{Method: "GET", Path: "/users/{id}"},
	))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if count != 2 || len(f.repository.replaced) != 2 || f.repository.replacedSrc != "shop.yaml" {
		t.Fatalf("count = %d, replaced = %d from %q", count, len(f.repository.replaced), f.repository.replacedSrc)
	}

	first := f.repository.replaced[0]
	if first.Path != "/orders" || first.Source != "shop.yaml" || first.ProjectID != f.projectID {
		t.Errorf("first = %+v", first)
	}
	if first.ImportedBy == nil || *first.ImportedBy != f.who.UserID || !first.CreatedAt.Equal(time.Unix(100, 0)) {
		t.Errorf("who and when = %v, %v", first.ImportedBy, first.CreatedAt)
	}
	if first.ID == uuid.Nil || first.ID == f.repository.replaced[1].ID {
		t.Errorf("the endpoints should each get an id of their own")
	}
}

func TestImportReadsNothingForSomebodyWhoCannotManageScenarios(t *testing.T) {
	f := newFixture(false)

	_, err := f.service.Import(context.Background(), f.who, f.request(domain.EndpointRef{Method: "GET", Path: "/users/{id}"}))
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("error = %v, wanted ErrForbidden", err)
	}
	if f.reader.calls != 0 || f.repository.replaceCalls != 0 {
		t.Errorf("read %d times, wrote %d times", f.reader.calls, f.repository.replaceCalls)
	}
}

func TestImportRefusesAChoiceOfNothingInsteadOfEmptyingTheSource(t *testing.T) {
	f := newFixture(true)

	_, err := f.service.Import(context.Background(), f.who, f.request())
	if !errors.Is(err, domain.ErrInvalidSpec) {
		t.Fatalf("error = %v, wanted ErrInvalidSpec", err)
	}
	if f.repository.replaceCalls != 0 {
		t.Error("an empty choice reached the repository, where it would have emptied the source")
	}
}

func TestImportRefusesAnEndpointTheFileDoesNotHaveAndWritesNothing(t *testing.T) {
	f := newFixture(true)

	_, err := f.service.Import(context.Background(), f.who, f.request(
		domain.EndpointRef{Method: "GET", Path: "/users/{id}"},
		domain.EndpointRef{Method: "GET", Path: "/nothing"},
	))
	if !errors.Is(err, domain.ErrInvalidSpec) || f.repository.replaceCalls != 0 {
		t.Fatalf("error = %v, writes = %d", err, f.repository.replaceCalls)
	}
}

func TestImportRefusesASourceWithNoName(t *testing.T) {
	f := newFixture(true)
	request := f.request(domain.EndpointRef{Method: "GET", Path: "/users/{id}"})
	request.Source = "  "

	if _, err := f.service.Import(context.Background(), f.who, request); !errors.Is(err, domain.ErrInvalidCatalog) {
		t.Fatalf("error = %v, wanted ErrInvalidCatalog", err)
	}
}

func TestImportPassesOnWhatTheReaderRefused(t *testing.T) {
	f := newFixture(true)
	f.reader.err = domain.ErrInvalidSpec

	_, err := f.service.Import(context.Background(), f.who, f.request(domain.EndpointRef{Method: "GET", Path: "/users/{id}"}))
	if !errors.Is(err, domain.ErrInvalidSpec) || f.repository.replaceCalls != 0 {
		t.Fatalf("error = %v, writes = %d", err, f.repository.replaceCalls)
	}
}

func TestImportNeverKeepsACredentialHeaderEvenIfTheReaderLetOneThrough(t *testing.T) {
	f := newFixture(true)
	f.reader.spec.Endpoints[0].Headers = map[string]string{"Authorization": "Bearer s3cr3t"}

	_, err := f.service.Import(context.Background(), f.who, f.request(domain.EndpointRef{Method: "GET", Path: "/users/{id}"}))
	if !errors.Is(err, domain.ErrInvalidCatalog) || f.repository.replaceCalls != 0 {
		t.Fatalf("error = %v, writes = %d", err, f.repository.replaceCalls)
	}
}

func TestListNeedsOnlyTheRightToViewTheProject(t *testing.T) {
	f := newFixture(true)
	filter := domain.CatalogFilter{Query: "users", Tag: "users"}

	if _, err := f.service.List(context.Background(), f.who, f.projectID, filter); err != nil {
		t.Fatalf("List: %v", err)
	}
	if f.repository.listFilter != filter {
		t.Errorf("filter = %+v", f.repository.listFilter)
	}

	denied := newFixture(false)
	if _, err := denied.service.List(context.Background(), denied.who, denied.projectID, filter); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("error = %v, wanted ErrForbidden", err)
	}
}

func TestDeletingNeedsTheRightToManageScenarios(t *testing.T) {
	denied := newFixture(false)

	if err := denied.service.Delete(context.Background(), denied.who, denied.projectID, uuid.New()); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("Delete: %v", err)
	}
	if _, err := denied.service.DeleteSource(context.Background(), denied.who, denied.projectID, "shop.yaml"); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("DeleteSource: %v", err)
	}
	if denied.repository.deletedSource != "" || denied.repository.deletedID != uuid.Nil {
		t.Error("something was deleted without the right to")
	}

	allowed := newFixture(true)
	count, err := allowed.service.DeleteSource(context.Background(), allowed.who, allowed.projectID, "shop.yaml")
	if err != nil || count != 3 {
		t.Errorf("DeleteSource = %d, %v", count, err)
	}
}

func collectionFixture(allowed bool) (fixture, *fakeCollections) {
	f := newFixture(allowed)
	collections := &fakeCollections{collection: domain.ImportedCollection{
		Requests: []domain.CollectionRequest{
			{Position: 0, Name: "Login", Method: "POST", URL: "{{baseUrl}}/login", Folders: []string{"Auth"},
				Headers: []domain.CollectionPair{{Name: "Authorization", Value: "Bearer ghp_ThisTokenMustNeverBeStored0123456789"}}},
			{Position: 1, Name: "Login again", Method: "POST", URL: "{{baseUrl}}/login"},
			{Position: 2, Name: "List", Method: "GET", URL: "{{baseUrl}}/orders"},
		},
	}}
	f.service.collections = CollectionReaders{Postman: collections, HAR: collections, Curl: collections}
	return f, collections
}

func (f fixture) collectionRequest(positions ...int) CollectionImport {
	return CollectionImport{ProjectID: f.projectID, Format: domain.FormatPostman, Source: "Shop", Collection: []byte("c"), Requests: positions}
}

func TestImportingACollectionKeepsEachMethodAndPathOnceAndSaysSo(t *testing.T) {
	f, _ := collectionFixture(true)

	result, err := f.service.ImportCollection(context.Background(), f.who, f.collectionRequest(0, 1, 2))
	if err != nil {
		t.Fatalf("ImportCollection: %v", err)
	}
	if result.Count != 2 || len(f.repository.replaced) != 2 {
		t.Fatalf("count = %d, stored = %d", result.Count, len(f.repository.replaced))
	}
	if f.repository.replaced[0].Tag != "Auth" || f.repository.replaced[0].Source != "Shop" {
		t.Errorf("first = %+v", f.repository.replaced[0])
	}

	said := false
	for _, finding := range result.Findings {
		if finding.Request == "Login again" {
			said = true
		}
	}
	if !said {
		t.Errorf("the repeated request was not reported: %+v", result.Findings)
	}
}

func TestACollectionsCredentialIsNeverStoredInTheCatalog(t *testing.T) {
	f, _ := collectionFixture(true)

	if _, err := f.service.ImportCollection(context.Background(), f.who, f.collectionRequest(0)); err != nil {
		t.Fatalf("ImportCollection: %v", err)
	}
	for _, endpoint := range f.repository.replaced {
		for name, value := range endpoint.Headers {
			t.Errorf("the catalog kept the header %s: %s", name, value)
		}
	}
}

func TestImportingACollectionReadsNothingWithoutTheRightAndRefusesAChoiceOfNothing(t *testing.T) {
	denied, collections := collectionFixture(false)
	if _, err := denied.service.ImportCollection(context.Background(), denied.who, denied.collectionRequest(0)); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("error = %v, wanted ErrForbidden", err)
	}
	if collections.calls != 0 || denied.repository.replaceCalls != 0 {
		t.Errorf("read %d times, wrote %d", collections.calls, denied.repository.replaceCalls)
	}

	allowed, _ := collectionFixture(true)
	if _, err := allowed.service.ImportCollection(context.Background(), allowed.who, allowed.collectionRequest()); !errors.Is(err, domain.ErrInvalidCollection) {
		t.Errorf("an empty choice: %v, wanted ErrInvalidCollection", err)
	}
	if allowed.repository.replaceCalls != 0 {
		t.Error("an empty choice reached the repository")
	}
}

func TestAFormatWithoutAReaderIsRefusedBeforeAnythingIsStored(t *testing.T) {
	f, collections := collectionFixture(true)
	f.service.collections = CollectionReaders{Postman: collections}

	request := f.collectionRequest(0)
	request.Format = domain.FormatHAR
	if _, err := f.service.ImportCollection(context.Background(), f.who, request); !errors.Is(err, domain.ErrInvalidCollection) {
		t.Errorf("error = %v, wanted a refusal", err)
	}
	if collections.calls != 0 || len(f.repository.replaced) != 0 {
		t.Errorf("read %d times, stored %d", collections.calls, len(f.repository.replaced))
	}
}

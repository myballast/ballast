//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/adapter/postgres"
	"github.com/youssefmahersi/ballast/internal/domain"
)

func catalogEndpoint(who domain.Identity, project uuid.UUID, source, method, path, tag string) domain.CatalogEndpoint {
	importedBy := who.UserID
	return domain.CatalogEndpoint{
		ImportedEndpoint: domain.ImportedEndpoint{Method: method, Path: path, Tag: tag},
		ID:               uuid.New(),
		ProjectID:        project,
		Source:           source,
		ImportedBy:       &importedBy,
		CreatedAt:        time.Now().UTC().Truncate(time.Microsecond),
	}
}

func listAll(ctx context.Context, t *testing.T, repo *postgres.CatalogRepository, who domain.Identity, project uuid.UUID) []domain.CatalogEndpoint {
	t.Helper()

	found, err := repo.List(ctx, who.OrgID, project, domain.CatalogFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	return found
}

func TestAnEndpointComesBackAsItWasWrittenWithItsHeadersAndBody(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	who, project := newEnvironmentWorld(ctx, t, db)
	repo := postgres.NewCatalogRepository(db)

	written := catalogEndpoint(who, project, "shop.yaml", "POST", "/orders/{id}", "orders")
	written.Summary = "Place an order"
	written.PathParams = []string{"id"}
	written.Headers = map[string]string{"X-Trace": "abc", "Accept": "application/json"}
	written.ContentType = "application/json"
	written.Body = `{"item":"thing"}`

	if err := repo.ReplaceSource(ctx, who.OrgID, project, "shop.yaml", []domain.CatalogEndpoint{written}); err != nil {
		t.Fatalf("ReplaceSource: %v", err)
	}

	found := listAll(ctx, t, repo, who, project)
	if len(found) != 1 {
		t.Fatalf("%d endpoints, wanted 1", len(found))
	}

	got := found[0]
	if got.ID != written.ID || got.Source != "shop.yaml" || got.Method != "POST" || got.Path != "/orders/{id}" {
		t.Errorf("identity = %+v", got)
	}
	if got.Tag != "orders" || got.Summary != "Place an order" || got.ContentType != "application/json" || got.Body != written.Body {
		t.Errorf("content = %+v", got)
	}
	if len(got.PathParams) != 1 || got.PathParams[0] != "id" {
		t.Errorf("path params = %v", got.PathParams)
	}
	if got.Headers["X-Trace"] != "abc" || got.Headers["Accept"] != "application/json" || len(got.Headers) != 2 {
		t.Errorf("headers = %v", got.Headers)
	}
	if got.ImportedBy == nil || *got.ImportedBy != who.UserID {
		t.Errorf("imported by = %v", got.ImportedBy)
	}
}

func TestReplacingASourceLeavesTheOthersAndDropsWhatItNoLongerHas(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	who, project := newEnvironmentWorld(ctx, t, db)
	repo := postgres.NewCatalogRepository(db)

	if err := repo.ReplaceSource(ctx, who.OrgID, project, "shop.yaml", []domain.CatalogEndpoint{
		catalogEndpoint(who, project, "shop.yaml", "GET", "/old", "t"),
		catalogEndpoint(who, project, "shop.yaml", "GET", "/kept", "t"),
	}); err != nil {
		t.Fatalf("first import: %v", err)
	}
	if err := repo.ReplaceSource(ctx, who.OrgID, project, "billing.yaml", []domain.CatalogEndpoint{
		catalogEndpoint(who, project, "billing.yaml", "GET", "/invoices", "billing"),
	}); err != nil {
		t.Fatalf("other source: %v", err)
	}

	if err := repo.ReplaceSource(ctx, who.OrgID, project, "shop.yaml", []domain.CatalogEndpoint{
		catalogEndpoint(who, project, "shop.yaml", "GET", "/kept", "t"),
		catalogEndpoint(who, project, "shop.yaml", "GET", "/new", "t"),
	}); err != nil {
		t.Fatalf("second import: %v", err)
	}

	var paths []string
	for _, endpoint := range listAll(ctx, t, repo, who, project) {
		paths = append(paths, endpoint.Source+" "+endpoint.Path)
	}
	wanted := "[billing.yaml /invoices shop.yaml /kept shop.yaml /new]"
	if fmt.Sprint(paths) != wanted {
		t.Errorf("endpoints = %v, wanted %s", paths, wanted)
	}
}

// The catalog has a ceiling, and an import that would cross it must not leave
// the source half replaced.
func TestAnImportPastTheCeilingChangesNothing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	db := openDB(ctx, t)
	who, project := newEnvironmentWorld(ctx, t, db)
	repo := postgres.NewCatalogRepository(db)

	if err := repo.ReplaceSource(ctx, who.OrgID, project, "shop.yaml", []domain.CatalogEndpoint{
		catalogEndpoint(who, project, "shop.yaml", "GET", "/kept", "t"),
	}); err != nil {
		t.Fatalf("first import: %v", err)
	}

	tooMany := make([]domain.CatalogEndpoint, 0, domain.MaxCatalogEndpoints+1)
	for i := 0; i <= domain.MaxCatalogEndpoints; i++ {
		tooMany = append(tooMany, catalogEndpoint(who, project, "shop.yaml", "GET", fmt.Sprintf("/e%d", i), "t"))
	}

	err := repo.ReplaceSource(ctx, who.OrgID, project, "shop.yaml", tooMany)
	if !errors.Is(err, domain.ErrInvalidCatalog) {
		t.Fatalf("error = %v, wanted ErrInvalidCatalog", err)
	}

	found := listAll(ctx, t, repo, who, project)
	if len(found) != 1 || found[0].Path != "/kept" {
		t.Errorf("the source holds %d endpoints after a refused import, wanted its old one", len(found))
	}
}

func TestListingFiltersByTagSourceAndAPlainSubstring(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	who, project := newEnvironmentWorld(ctx, t, db)
	repo := postgres.NewCatalogRepository(db)

	users := catalogEndpoint(who, project, "shop.yaml", "GET", "/users/{id}", "users")
	users.Summary = "Read one user"
	orders := catalogEndpoint(who, project, "shop.yaml", "POST", "/orders", "orders")
	invoices := catalogEndpoint(who, project, "billing.yaml", "GET", "/invoices", "")

	if err := repo.ReplaceSource(ctx, who.OrgID, project, "shop.yaml", []domain.CatalogEndpoint{users, orders}); err != nil {
		t.Fatalf("shop: %v", err)
	}
	if err := repo.ReplaceSource(ctx, who.OrgID, project, "billing.yaml", []domain.CatalogEndpoint{invoices}); err != nil {
		t.Fatalf("billing: %v", err)
	}

	cases := map[string]struct {
		filter domain.CatalogFilter
		wanted string
	}{
		"nothing":        {domain.CatalogFilter{}, "[/orders /users/{id} /invoices]"},
		"by tag":         {domain.CatalogFilter{Tag: "users"}, "[/users/{id}]"},
		"by source":      {domain.CatalogFilter{Source: "billing.yaml"}, "[/invoices]"},
		"in the summary": {domain.CatalogFilter{Query: "READ ONE"}, "[/users/{id}]"},
		"in the method":  {domain.CatalogFilter{Query: "post"}, "[/orders]"},
		"a percent sign": {domain.CatalogFilter{Query: "%"}, "[]"},
		"an underscore":  {domain.CatalogFilter{Query: "_"}, "[]"},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			found, err := repo.List(ctx, who.OrgID, project, c.filter)
			if err != nil {
				t.Fatalf("List: %v", err)
			}

			paths := []string{}
			for _, endpoint := range found {
				paths = append(paths, endpoint.Path)
			}
			if fmt.Sprint(paths) != c.wanted {
				t.Errorf("paths = %v, wanted %s", paths, c.wanted)
			}
		})
	}
}

func TestOneProjectsEndpointsAreNotAnotherProjectsEvenInTheSameOrganization(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	who, project := newEnvironmentWorld(ctx, t, db)
	repo := postgres.NewCatalogRepository(db)

	otherProject := uuid.New()
	if err := db.InTenantTx(ctx, who.OrgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO project (id, org_id, name, slug) VALUES ($1, $2, 'q', $3)`,
			otherProject, who.OrgID, otherProject.String())
		return err
	}); err != nil {
		t.Fatalf("second project: %v", err)
	}

	mine := catalogEndpoint(who, project, "shop.yaml", "GET", "/mine", "t")
	if err := repo.ReplaceSource(ctx, who.OrgID, project, "shop.yaml", []domain.CatalogEndpoint{mine}); err != nil {
		t.Fatalf("ReplaceSource: %v", err)
	}

	if found := listAll(ctx, t, repo, who, otherProject); len(found) != 0 {
		t.Errorf("the other project lists %d endpoints of this one", len(found))
	}
	if err := repo.Delete(ctx, who.OrgID, otherProject, mine.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("deleting through the other project: %v, wanted ErrNotFound", err)
	}
	if removed, err := repo.DeleteSource(ctx, who.OrgID, otherProject, "shop.yaml"); err != nil || removed != 0 {
		t.Errorf("deleting the source through the other project: %d, %v", removed, err)
	}

	// Importing the same source name in the other project does not touch this one.
	if err := repo.ReplaceSource(ctx, who.OrgID, otherProject, "shop.yaml", []domain.CatalogEndpoint{
		catalogEndpoint(who, otherProject, "shop.yaml", "GET", "/theirs", "t"),
	}); err != nil {
		t.Fatalf("other import: %v", err)
	}
	if found := listAll(ctx, t, repo, who, project); len(found) != 1 || found[0].Path != "/mine" {
		t.Errorf("this project's endpoints changed: %v", found)
	}
}

func TestAnotherOrganizationSeesAndRemovesNothing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	ada, project := newEnvironmentWorld(ctx, t, db)
	mallory, _ := newEnvironmentWorld(ctx, t, db)
	repo := postgres.NewCatalogRepository(db)

	mine := catalogEndpoint(ada, project, "shop.yaml", "GET", "/mine", "t")
	if err := repo.ReplaceSource(ctx, ada.OrgID, project, "shop.yaml", []domain.CatalogEndpoint{mine}); err != nil {
		t.Fatalf("ReplaceSource: %v", err)
	}

	if found := listAll(ctx, t, repo, mallory, project); len(found) != 0 {
		t.Errorf("another organization lists %d endpoints", len(found))
	}
	if err := repo.Delete(ctx, mallory.OrgID, project, mine.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Delete: %v, wanted ErrNotFound", err)
	}
	if removed, _ := repo.DeleteSource(ctx, mallory.OrgID, project, "shop.yaml"); removed != 0 {
		t.Errorf("DeleteSource removed %d", removed)
	}
	// Writing into somebody else's project is refused by the composite key.
	if err := repo.ReplaceSource(ctx, mallory.OrgID, project, "x.yaml", []domain.CatalogEndpoint{
		catalogEndpoint(mallory, project, "x.yaml", "GET", "/x", "t"),
	}); err == nil {
		t.Error("an organization wrote an endpoint into another's project")
	}

	if found := listAll(ctx, t, repo, ada, project); len(found) != 1 {
		t.Errorf("Ada's catalog holds %d endpoints, wanted 1", len(found))
	}
}

func TestDeletingOneEndpointAndOneSource(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	who, project := newEnvironmentWorld(ctx, t, db)
	repo := postgres.NewCatalogRepository(db)

	a := catalogEndpoint(who, project, "shop.yaml", "GET", "/a", "t")
	b := catalogEndpoint(who, project, "shop.yaml", "GET", "/b", "t")
	c := catalogEndpoint(who, project, "billing.yaml", "GET", "/c", "t")
	for source, endpoints := range map[string][]domain.CatalogEndpoint{"shop.yaml": {a, b}, "billing.yaml": {c}} {
		if err := repo.ReplaceSource(ctx, who.OrgID, project, source, endpoints); err != nil {
			t.Fatalf("ReplaceSource %s: %v", source, err)
		}
	}

	if err := repo.Delete(ctx, who.OrgID, project, a.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := repo.Delete(ctx, who.OrgID, project, a.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("deleting twice: %v, wanted ErrNotFound", err)
	}

	removed, err := repo.DeleteSource(ctx, who.OrgID, project, "shop.yaml")
	if err != nil || removed != 1 {
		t.Errorf("DeleteSource = %d, %v, wanted 1", removed, err)
	}

	if found := listAll(ctx, t, repo, who, project); len(found) != 1 || found[0].Path != "/c" {
		t.Errorf("what is left: %v", found)
	}
}

// Whoever imported an endpoint can be removed from the organization, and the
// endpoint stays: the foreign key blanks imported_by alone.
func TestRemovingThePersonWhoImportedKeepsTheEndpoint(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	ada, project := newEnvironmentWorld(ctx, t, db)
	grace := aSecondUser(ctx, t, db, ada, project)
	repo := postgres.NewCatalogRepository(db)

	if err := repo.ReplaceSource(ctx, ada.OrgID, project, "shop.yaml", []domain.CatalogEndpoint{
		catalogEndpoint(grace, project, "shop.yaml", "GET", "/a", "t"),
	}); err != nil {
		t.Fatalf("ReplaceSource: %v", err)
	}

	if err := db.InTenantTx(ctx, ada.OrgID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM membership WHERE user_id = $1`, grace.UserID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM app_user WHERE id = $1`, grace.UserID)
		return err
	}); err != nil {
		t.Fatalf("remove the person: %v", err)
	}

	found := listAll(ctx, t, repo, ada, project)
	if len(found) != 1 || found[0].ImportedBy != nil {
		t.Errorf("endpoints = %+v, wanted the endpoint with no importer", found)
	}
}

func TestAThousandEndpointsAreWrittenInWellUnderASecond(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := openDB(ctx, t)
	who, project := newEnvironmentWorld(ctx, t, db)
	repo := postgres.NewCatalogRepository(db)

	endpoints := make([]domain.CatalogEndpoint, 0, 1000)
	for i := 0; i < 1000; i++ {
		endpoints = append(endpoints, catalogEndpoint(who, project, "big.yaml", "GET", fmt.Sprintf("/e%d", i), "t"))
	}

	started := time.Now()
	if err := repo.ReplaceSource(ctx, who.OrgID, project, "big.yaml", endpoints); err != nil {
		t.Fatalf("ReplaceSource: %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Errorf("writing a thousand endpoints took %v", elapsed)
	}
}

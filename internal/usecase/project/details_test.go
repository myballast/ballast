package project

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

func projectsWith(ps ...domain.Project) *fakeProjects {
	byID := map[uuid.UUID]domain.Project{}
	for _, p := range ps {
		byID[p.ID] = p
	}
	return &fakeProjects{byID: byID}
}

func aProject(slug string) domain.Project {
	return domain.Project{ID: uuid.New(), Name: slug, Slug: slug}
}

func TestCreateKeepsTheDetailsInTheFormTheyAreStored(t *testing.T) {
	projects := projectsWith()
	service := newService(projects, &fakeMemberships{}, &allowAll{})

	created, err := service.Create(context.Background(), someone(), "Storefront", "storefront", domain.ProjectDetails{
		Infra: " k8s ", Region: "eu-west-1", Stack: []string{"Go", "pg", "go"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if created.Details.Infra != "k8s" || len(created.Details.Stack) != 2 || created.Details.Stack[0] != "go" {
		t.Errorf("stored %+v", created.Details)
	}
}

func TestCreateRefusesDetailsItWillNotStore(t *testing.T) {
	projects := projectsWith()
	service := newService(projects, &fakeMemberships{}, &allowAll{})

	_, err := service.Create(context.Background(), someone(), "Storefront", "storefront",
		domain.ProjectDetails{Stack: []string{"not a label"}})
	if !errors.Is(err, domain.ErrInvalidProject) {
		t.Fatalf("got %v", err)
	}
	if projects.created != nil {
		t.Error("a project was stored with a stack Ballast refuses")
	}
}

// A field left out stays; a field sent empty is cleared. Two different things,
// and the only way to tell "I did not touch it" from "I removed it".
func TestUpdateLeavesWhatItIsNotToldToChange(t *testing.T) {
	p := aProject("storefront")
	p.Details = domain.ProjectDetails{Infra: "k8s", Region: "eu-west-1", Owner: "payments", Stack: []string{"go", "pg"}}
	service := newService(projectsWith(p), &fakeMemberships{}, &allowAll{})

	infra := "railway"
	updated, err := service.Update(context.Background(), someone(), p.ID, Changes{Infra: &infra})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	d := updated.Details
	if d.Infra != "railway" || d.Region != "eu-west-1" || d.Owner != "payments" || len(d.Stack) != 2 {
		t.Errorf("details after changing only the infra: %+v", d)
	}
	if updated.Name != "storefront" || updated.Slug != "storefront" {
		t.Errorf("name and slug moved: %q %q", updated.Name, updated.Slug)
	}
}

func TestUpdateClearsWhatIsSentEmpty(t *testing.T) {
	p := aProject("storefront")
	p.Details = domain.ProjectDetails{Infra: "k8s", Owner: "payments", Stack: []string{"go"}}
	service := newService(projectsWith(p), &fakeMemberships{}, &allowAll{})

	none, noStack := "", []string{}
	updated, err := service.Update(context.Background(), someone(), p.ID, Changes{Infra: &none, Owner: &none, Stack: &noStack})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if d := updated.Details; d.Infra != "" || d.Owner != "" || len(d.Stack) != 0 || d.Place() != "" {
		t.Errorf("details after clearing: %+v", d)
	}
}

func TestUpdateRefusesBadDetailsAndWritesNothing(t *testing.T) {
	p := aProject("storefront")
	projects := projectsWith(p)
	service := newService(projects, &fakeMemberships{}, &allowAll{})

	long := string(make([]byte, 100))
	if _, err := service.Update(context.Background(), someone(), p.ID, Changes{Owner: &long}); err == nil {
		t.Fatal("a 100-character owner was accepted")
	}
	if projects.byID[p.ID].Details.Owner != "" {
		t.Error("a refused update was stored")
	}
}

func TestOnlyAnAdministratorEditsTheDetails(t *testing.T) {
	p := aProject("storefront")
	service := newService(projectsWith(p), &fakeMemberships{}, refuseAll{})

	infra := "k8s"
	if _, err := service.Update(context.Background(), someone(), p.ID, Changes{Infra: &infra}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Favourites
// ---------------------------------------------------------------------------

func TestAFavouriteIsTheCallersAlone(t *testing.T) {
	p := aProject("storefront")
	service := newService(projectsWith(p), &fakeMemberships{}, &allowAll{})
	ada, grace := someone(), someone()

	if err := service.SetFavorite(context.Background(), ada, p.ID, true); err != nil {
		t.Fatalf("SetFavorite: %v", err)
	}

	listedForAda, _ := service.List(context.Background(), ada)
	listedForGrace, _ := service.List(context.Background(), grace)
	if len(listedForAda) != 1 || !listedForAda[0].Favorite {
		t.Errorf("Ada does not see her own star: %+v", listedForAda)
	}
	if len(listedForGrace) != 1 || listedForGrace[0].Favorite {
		t.Errorf("Grace sees Ada's star: %+v", listedForGrace)
	}

	foundForGrace, err := service.ByID(context.Background(), grace, p.ID)
	if err != nil || foundForGrace.Favorite {
		t.Errorf("a single project read shows Ada's star to Grace: %+v %v", foundForGrace, err)
	}
	foundForAda, _ := service.ByID(context.Background(), ada, p.ID)
	if !foundForAda.Favorite {
		t.Error("a single project read does not show Ada's own star")
	}
}

func TestAStarCanBeTakenBackAndSetTwice(t *testing.T) {
	p := aProject("storefront")
	service := newService(projectsWith(p), &fakeMemberships{}, &allowAll{})
	ada := someone()

	for _, on := range []bool{true, true, false} {
		if err := service.SetFavorite(context.Background(), ada, p.ID, on); err != nil {
			t.Fatalf("SetFavorite(%v): %v", on, err)
		}
	}
	listed, _ := service.List(context.Background(), ada)
	if listed[0].Favorite {
		t.Error("the star is still there")
	}
}

func TestSomebodyWhoDoesNotBelongCannotStarAProject(t *testing.T) {
	p := aProject("storefront")
	projects := projectsWith(p)
	service := newService(projects, &fakeMemberships{}, refuseAll{})

	if err := service.SetFavorite(context.Background(), someone(), p.ID, true); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("got %v", err)
	}
	if len(projects.stars) != 0 {
		t.Errorf("a star was written: %v", projects.stars)
	}
}

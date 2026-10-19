//go:build integration

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

type listedProject struct {
	ID       string   `json:"id"`
	Favorite bool     `json:"favorite"`
	Infra    string   `json:"infra"`
	Region   string   `json:"region"`
	Owner    string   `json:"owner"`
	Stack    []string `json:"stack"`
}

func (w membersWorld) projects(t *testing.T, as *http.Cookie) []listedProject {
	t.Helper()

	var out struct {
		Projects []listedProject `json:"projects"`
	}
	if status, _ := w.call(t, as, "GET", "/projects", "", &out); status != http.StatusOK {
		t.Fatalf("GET /projects: %d", status)
	}
	return out.Projects
}

// The acceptance criterion through the API it is served by: one person's star is
// not another's, in the list and on the project itself.
func TestAFavouriteOfOnePersonNeverReachesAnother(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	w := newMembersWorld(ctx, t)

	admin, viewer := w.cookie(t, w.admin), w.cookie(t, w.viewer)
	favorite := fmt.Sprintf("/projects/%s/favorite", w.project)

	if status, _ := w.call(t, admin, "PUT", favorite, "", nil); status != http.StatusNoContent {
		t.Fatalf("the administrator starring got %d, want 204", status)
	}
	// Twice is the same as once.
	if status, _ := w.call(t, admin, "PUT", favorite, "", nil); status != http.StatusNoContent {
		t.Fatalf("starring twice got %d, want 204", status)
	}

	if mine := w.projects(t, admin); len(mine) != 1 || !mine[0].Favorite {
		t.Errorf("the administrator's own star: %+v", mine)
	}
	if theirs := w.projects(t, viewer); len(theirs) != 1 || theirs[0].Favorite {
		t.Errorf("the viewer sees the administrator's star: %+v", theirs)
	}

	var seen listedProject
	w.call(t, viewer, "GET", fmt.Sprintf("/projects/%s", w.project), "", &seen)
	if seen.Favorite {
		t.Error("the project read shows the administrator's star to the viewer")
	}

	// A viewer may star it for themselves — a star grants nothing — and
	// unstarring is theirs only.
	if status, _ := w.call(t, viewer, "PUT", favorite, "", nil); status != http.StatusNoContent {
		t.Fatalf("a viewer starring got %d, want 204", status)
	}
	if status, _ := w.call(t, viewer, "DELETE", favorite, "", nil); status != http.StatusNoContent {
		t.Fatalf("a viewer unstarring got %d, want 204", status)
	}
	if mine := w.projects(t, admin); !mine[0].Favorite {
		t.Error("the viewer unstarring took the administrator's star")
	}

	// Somebody outside the project cannot star it.
	if status, _ := w.call(t, w.cookie(t, w.mallory), "PUT", favorite, "", nil); status != http.StatusForbidden {
		t.Errorf("an outsider starring got %d, want 403", status)
	}
	if status, _ := w.call(t, nil, "PUT", favorite, "", nil); status != http.StatusUnauthorized {
		t.Errorf("an anonymous star got %d, want 401", status)
	}
}

func TestOnlyAnAdministratorEditsAProjectsDetails(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	w := newMembersWorld(ctx, t)

	project := fmt.Sprintf("/projects/%s", w.project)
	body := `{"infra":"k8s","region":"eu-west-1","owner":"Payments team","stack":["Go","pg","go"]}`

	for name, who := range map[string]*http.Cookie{"maintainer": w.cookie(t, w.maintainer), "viewer": w.cookie(t, w.viewer)} {
		if status, _ := w.call(t, who, "PATCH", project, body, nil); status != http.StatusForbidden {
			t.Errorf("a %s editing the details got %d, want 403", name, status)
		}
	}

	admin := w.cookie(t, w.admin)
	var edited listedProject
	if status, _ := w.call(t, admin, "PATCH", project, body, &edited); status != http.StatusOK {
		t.Fatalf("the administrator editing got %d", status)
	}
	if edited.Infra != "k8s" || edited.Owner != "Payments team" || len(edited.Stack) != 2 || edited.Stack[0] != "go" {
		t.Errorf("edited: %+v", edited)
	}

	// Changing one field leaves the others; the name is left alone.
	var again listedProject
	w.call(t, admin, "PATCH", project, `{"region":"us-east-1"}`, &again)
	if again.Region != "us-east-1" || again.Infra != "k8s" || len(again.Stack) != 2 {
		t.Errorf("after changing only the region: %+v", again)
	}

	// Empty clears, and a project with nothing said reads as empty fields and an
	// empty array — never null.
	var cleared listedProject
	w.call(t, admin, "PATCH", project, `{"infra":"","region":"","owner":"","stack":[]}`, &cleared)
	if cleared.Infra != "" || cleared.Stack == nil || len(cleared.Stack) != 0 {
		t.Errorf("cleared: %+v", cleared)
	}

	for name, bad := range map[string]string{
		"a label with a space": `{"stack":["node js"]}`,
		"an owner too long":    `{"owner":"` + string(make([]byte, 200)) + `"}`,
	} {
		if status, _ := w.call(t, admin, "PATCH", project, bad, nil); status != http.StatusBadRequest {
			t.Errorf("%s answered %d, want 400", name, status)
		}
	}
}

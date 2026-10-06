package http_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	httpadapter "github.com/youssefmahersi/ballast/internal/adapter/http"
)

// The API's contract lives in docs/api/openapi.yaml, written by hand. These
// tests are what keep it true: a route added to router.go without being
// documented, a documented route that no longer exists, or a route whose
// documented authentication differs from what the router enforces, each fails
// the build.

const specPath = "../../../docs/api/openapi.yaml"

// route is one method and path, written the way net/http patterns write it:
// "GET /runs/{id}".
type route struct {
	key    string
	public bool
}

// registeredRoutes returns every route the API serves, read from the table in
// routes.go.
//
// Reading the table rather than the source of the registration is what the
// table is for: it is the only place a route comes from, and routes_test.go is
// what keeps that true.
func registeredRoutes(t *testing.T) []route {
	t.Helper()

	declared := httpadapter.Routes()
	if len(declared) == 0 {
		t.Fatal("the route table is empty; the test would prove nothing")
	}

	routes := make([]route, 0, len(declared))
	for _, declaration := range declared {
		routes = append(routes, route{
			key:    declaration.Method + " " + declaration.Path,
			public: declaration.Scope == httpadapter.ScopePublic,
		})
	}
	return routes
}

type operation struct {
	OperationID string                 `yaml:"operationId"`
	Security    *[]map[string][]string `yaml:"security"`
	Responses   map[string]any         `yaml:"responses"`
}

// A path item holds its operations by method, and may also hold keys that are
// not operations at all — `parameters`, shared by every method under it, is a
// sequence rather than a mapping. Decoding each method key on its own keeps
// those from being mistaken for operations.
type spec struct {
	Security []map[string][]string           `yaml:"security"`
	Paths    map[string]map[string]yaml.Node `yaml:"paths"`
}

var httpMethods = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}

// documentedRoutes reads the spec and returns every operation in it.
func documentedRoutes(t *testing.T) []route {
	t.Helper()

	raw, err := os.ReadFile(filepath.Clean(specPath))
	if err != nil {
		t.Fatalf("read the spec: %v", err)
	}

	var doc spec
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("the spec is not valid YAML: %v", err)
	}
	if len(doc.Security) == 0 {
		t.Fatal("the spec declares no default security; every route would read as public")
	}

	var routes []route
	for path, item := range doc.Paths {
		for method, node := range item {
			if !slices.Contains(httpMethods, method) {
				continue
			}

			var op operation
			if err := node.Decode(&op); err != nil {
				t.Errorf("%s %s cannot be read: %v", strings.ToUpper(method), path, err)
				continue
			}

			if op.OperationID == "" {
				t.Errorf("%s %s has no operationId", strings.ToUpper(method), path)
			}
			if len(op.Responses) == 0 {
				t.Errorf("%s %s documents no response", strings.ToUpper(method), path)
			}
			public := op.Security != nil && len(*op.Security) == 0
			routes = append(routes, route{key: strings.ToUpper(method) + " " + path, public: public})
		}
	}
	return routes
}

func TestEveryRouteIsDocumented(t *testing.T) {
	documented := map[string]route{}
	for _, r := range documentedRoutes(t) {
		documented[r.key] = r
	}

	for _, r := range registeredRoutes(t) {
		doc, ok := documented[r.key]
		if !ok {
			t.Errorf("%s is registered in router.go but missing from docs/api/openapi.yaml", r.key)
			continue
		}
		if doc.public != r.public {
			t.Errorf("%s: the router makes it %s, the spec says %s", r.key, visibility(r.public), visibility(doc.public))
		}
	}
}

func TestEveryDocumentedRouteExists(t *testing.T) {
	registered := map[string]bool{}
	for _, r := range registeredRoutes(t) {
		registered[r.key] = true
	}

	for _, r := range documentedRoutes(t) {
		if !registered[r.key] {
			t.Errorf("%s is documented in docs/api/openapi.yaml but not registered in router.go", r.key)
		}
	}
}

// Path parameters are named the same on both sides, which is what lets the two
// lists above be compared as strings.
func TestPathParametersAreDeclared(t *testing.T) {
	raw, err := os.ReadFile(filepath.Clean(specPath))
	if err != nil {
		t.Fatalf("read the spec: %v", err)
	}
	var doc struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse: %v", err)
	}

	placeholder := regexp.MustCompile(`\{([^}]+)\}`)
	for path := range doc.Paths {
		for _, m := range placeholder.FindAllStringSubmatch(path, -1) {
			if !strings.Contains(string(raw), "name: "+m[1]) {
				t.Errorf("%s uses {%s}, which no parameter declares", path, m[1])
			}
		}
	}
}

func visibility(public bool) string {
	if public {
		return "public"
	}
	return "behind a session"
}

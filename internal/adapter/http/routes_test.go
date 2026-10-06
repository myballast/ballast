package http_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	httpadapter "github.com/youssefmahersi/ballast/internal/adapter/http"
	"github.com/youssefmahersi/ballast/internal/domain"
)

// The table in routes.go is the only place a route comes from. Everything below
// holds it to that, because a route added without a declared permission is the
// mistake this whole ticket exists to make impossible.

func TestRoutesAreDeclared(t *testing.T) {
	routes := httpadapter.Routes()
	if len(routes) == 0 {
		t.Fatal("the table is empty; every test here would prove nothing")
	}

	seen := map[string]bool{}
	for _, route := range routes {
		key := route.Method + " " + route.Path

		if seen[key] {
			t.Errorf("%s is declared twice", key)
		}
		seen[key] = true

		if route.Method == "" || route.Path == "" {
			t.Errorf("a route has no method or no path: %+v", route)
		}
		if !strings.HasPrefix(route.Path, "/") {
			t.Errorf("%s does not start with a slash", key)
		}
	}
}

// Every route that requires a session says what it requires beyond one. The
// three scopes that name a permission must carry one; the one that does not must
// say why, in writing, so that "no permission" reads as a decision rather than
// as something nobody got round to.
func TestEveryAuthenticatedRouteDeclaresItsPermission(t *testing.T) {
	for _, route := range httpadapter.Routes() {
		key := route.Method + " " + route.Path

		switch route.Scope {
		case httpadapter.ScopePublic:
			if route.Permission != 0 {
				t.Errorf("%s is public and names a permission", key)
			}

		case httpadapter.ScopeNone:
			if route.Permission != 0 {
				t.Errorf("%s needs no permission and names one", key)
			}
			if strings.TrimSpace(route.Reason) == "" {
				t.Errorf("%s requires no permission and does not say why", key)
			}

		case httpadapter.ScopePath, httpadapter.ScopeAnyProject, httpadapter.ScopeDerived:
			if route.Permission == 0 {
				t.Errorf("%s declares a scope but no permission", key)
			}

		default:
			t.Errorf("%s has no scope", key)
		}
	}
}

// A route whose project sits in the path says which path value holds it, and
// that value appears in the path. This is what lets the middleware enforce the
// declaration rather than leave it as a comment.
func TestPathScopedRoutesNameTheirProjectParameter(t *testing.T) {
	for _, route := range httpadapter.Routes() {
		if route.Scope != httpadapter.ScopePath {
			continue
		}

		key := route.Method + " " + route.Path
		if route.ProjectParam == "" {
			t.Errorf("%s takes its project from the path and does not say which parameter", key)
			continue
		}
		if !strings.Contains(route.Path, "{"+route.ProjectParam+"}") {
			t.Errorf("%s names {%s} and its path does not hold it", key, route.ProjectParam)
		}
	}
}

// The permission a route declares has to exist. A zero or unknown bit would
// read as a check that passes.
func TestDeclaredPermissionsAreReal(t *testing.T) {
	known := domain.PermissionSet(0)
	for _, perm := range []domain.Permission{
		domain.PermViewProject,
		domain.PermRunTest,
		domain.PermCancelRun,
		domain.PermManageScenarios,
		domain.PermManageEnvironments,
		domain.PermApproveTarget,
		domain.PermManageMembers,
		domain.PermManageProject,
	} {
		known |= domain.PermissionSet(perm)
	}

	for _, route := range httpadapter.Routes() {
		if route.Permission == 0 {
			continue
		}
		if !known.Has(route.Permission) {
			t.Errorf("%s %s requires bit %d, which no permission defines",
				route.Method, route.Path, int64(route.Permission))
		}
	}
}

// The table is the only way a route reaches the multiplexer. A registration made
// anywhere else would bypass every check above, so the source is read to make
// sure there is exactly one, and that it sits inside the loop that walks the
// table.
//
// Checking only that registrations live in NewRouter is not enough: a hand-added
// route would sit there too, which is the hole this is about.
func TestNoRouteIsRegisteredOutsideTheTable(t *testing.T) {
	fset := token.NewFileSet()
	pkg, err := parser.ParseDir(fset, ".", func(f os.FileInfo) bool {
		return !strings.HasSuffix(f.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse the package: %v", err)
	}

	found := 0
	for _, files := range pkg {
		for name, file := range files.Files {
			loop := tableLoop(file)

			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if selector.Sel.Name != "Handle" && selector.Sel.Name != "HandleFunc" {
					return true
				}

				found++

				if loop == nil {
					t.Errorf("%s registers a route and holds no loop over Routes()",
						filepath.Base(name))
					return true
				}
				if call.Pos() < loop.Pos() || call.End() > loop.End() {
					t.Errorf("%s:%d registers a route outside the loop over Routes(), "+
						"bypassing the table",
						filepath.Base(name), fset.Position(call.Pos()).Line)
				}
				return true
			})
		}
	}

	if found == 0 {
		t.Fatal("found no route registration at all; this test would prove nothing")
	}
	if found > 1 {
		t.Errorf("found %d route registrations; the table needs exactly one", found)
	}
}

// tableLoop finds the `for … := range Routes()` statement, which is the one
// place a route may be registered.
func tableLoop(file *ast.File) *ast.RangeStmt {
	var loop *ast.RangeStmt

	ast.Inspect(file, func(n ast.Node) bool {
		stmt, ok := n.(*ast.RangeStmt)
		if !ok {
			return true
		}
		call, ok := stmt.X.(*ast.CallExpr)
		if !ok {
			return true
		}
		if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == "Routes" {
			loop = stmt
			return false
		}
		return true
	})

	return loop
}

// The permission a route requires is the first thing somebody calling this API
// needs to know, so it is in the spec too — and the spec is held to the table
// rather than trusted.
func TestTheSpecDeclaresTheSamePermissions(t *testing.T) {
	raw, err := os.ReadFile(filepath.Clean("../../../docs/api/openapi.yaml"))
	if err != nil {
		t.Fatalf("read the spec: %v", err)
	}

	var doc struct {
		Paths map[string]map[string]yaml.Node `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("the spec is not valid YAML: %v", err)
	}

	documented := map[string]string{}
	for path, item := range doc.Paths {
		for method, node := range item {
			var op struct {
				Permission string `yaml:"x-ballast-permission"`
			}
			if err := node.Decode(&op); err != nil {
				continue
			}
			documented[strings.ToUpper(method)+" "+path] = op.Permission
		}
	}

	for _, route := range httpadapter.Routes() {
		if route.Scope == httpadapter.ScopePublic {
			continue
		}

		key := route.Method + " " + route.Path
		want := route.PermissionName()

		got, ok := documented[key]
		if !ok || got == "" {
			t.Errorf("%s requires %q and the spec does not say so", key, want)
			continue
		}
		if got != want {
			t.Errorf("%s requires %q and the spec says %q", key, want, got)
		}
	}
}

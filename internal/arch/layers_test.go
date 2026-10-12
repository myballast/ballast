package arch

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// layer is one package tree and what its files may import.
//
// denied is checked before anything else, including the standard library, so
// that net/http can be refused to a layer the standard library is otherwise
// open to — and so that the failure says what to do instead rather than only
// "not allowed".
type layer struct {
	// name is how the layer is spoken about in a failure.
	name string
	// dir is where it lives, relative to this package.
	dir string
	// allowed are import path prefixes this layer may use, besides the
	// standard library.
	allowed []string
	// denied are import path prefixes it may not, with the reason.
	denied map[string]string
}

const module = "github.com/youssefmahersi/ballast/"

// The two inner layers. The outer ones are not described here: an adapter may
// import the standard library, a driver, a client and the layers beneath it,
// which is nearly everything, and a rule that forbids nearly nothing is a rule
// nobody can rely on. What matters is that the two layers holding the business
// rules cannot reach outward.
var layers = []layer{
	{
		name: "internal/domain",
		dir:  filepath.Join("..", "domain"),
		// One third party, for identifiers. Nothing of the project at all:
		// the domain is what everything else is written in terms of.
		allowed: []string{"github.com/google/uuid"},
		denied: map[string]string{
			"database/sql":              "the domain must not know about SQL; declare a port in usecase instead",
			"net/http":                  "the domain must not know about HTTP; that belongs in adapter/http",
			"github.com/jackc/pgx":      "the domain must not know about the database driver",
			"github.com/docker/docker":  "the domain must not know what a container is",
			module + "internal/usecase": "the domain must not import an outer layer",
			module + "internal/adapter": "the domain must not import an outer layer",
			module + "internal/infra":   "the domain must not import an outer layer",
			module + "cmd":              "the domain must not import an outer layer",
		},
	},
	{
		name: "internal/usecase",
		dir:  filepath.Join("..", "usecase"),
		allowed: []string{
			"github.com/google/uuid",
			module + "internal/domain",
			module + "internal/usecase",
		},
		denied: map[string]string{
			module + "internal/adapter": "usecase must not import adapter; declare the interface in ports.go",
			module + "internal/infra":   "usecase must not import infra; declare the interface in ports.go",
			module + "cmd":              "usecase must not import an outer layer",
			"github.com/jackc/pgx":      "usecase must not know about the database driver",
			// The one the ticket is about. A usecase that drives containers
			// cannot be satisfied by a runner deployed somewhere else, which is
			// the whole reason the Executor port exists.
			"github.com/docker/docker": "usecase must not drive containers; go through the Executor port",
			"github.com/go-chi/chi":    "usecase must not know about the HTTP router",
			"net/http":                 "usecase must not speak HTTP; declare a port and implement it in adapter",
			"database/sql":             "usecase must not know about SQL; declare a repository port in ports.go",
			"os/exec":                  "usecase must not start processes; go through the Executor port",
		},
	},
}

// imports reads every Go file under dir, test files included, and hands each
// import to check.
//
// Test files are read on purpose. A fake that imported the Docker client would
// defeat the whole arrangement just as surely as the real code doing it, and it
// is the easier mistake to make.
func imports(t *testing.T, dir string, check func(file, path string)) int {
	t.Helper()

	var files int
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}

		// Only the import block is parsed: this says nothing about whether the
		// code compiles, and it must not need it to.
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}

		files++
		for _, spec := range parsed.Imports {
			check(path, strings.Trim(spec.Path.Value, `"`))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}

	return files
}

// standard reports whether an import path is the standard library.
//
// The test the toolchain itself uses: a standard library path has no dot in its
// first segment, because everything else is a domain name.
func standard(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

// TestTheInnerLayersImportNothingOutward is the rule, read off the source
// rather than trusted.
func TestTheInnerLayersImportNothingOutward(t *testing.T) {
	for _, current := range layers {
		t.Run(current.name, func(t *testing.T) {
			files := imports(t, current.dir, func(file, path string) {
				for denied, why := range current.denied {
					if path == denied || strings.HasPrefix(path, denied+"/") {
						t.Errorf("%s imports %s — %s", file, path, why)
						return
					}
				}

				if standard(path) {
					return
				}

				for _, allowed := range current.allowed {
					if path == allowed || strings.HasPrefix(path, allowed+"/") {
						return
					}
				}

				t.Errorf("%s imports %s, which %s may not: add it to this layer's allowed list "+
					"if it genuinely belongs there, or declare a port instead",
					file, path, current.name)
			})

			// The lesson of every "which rows break the rule?" test: against an
			// empty tree the loop above passes while proving nothing. A layer
			// of this project has dozens of files, so a handful means the walk
			// went somewhere else.
			if files < 10 {
				t.Fatalf("only %d Go file(s) were read under %s; the walk found the wrong place",
					files, current.dir)
			}
		})
	}
}

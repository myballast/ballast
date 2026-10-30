package main

import (
	"go/parser"
	"go/token"
	"testing"
)

// The worker is shipped in an image with no zone database, and it works out for
// itself whether a run falls inside an environment's window — in the window's own
// zone. Without the database inside the binary, every run against an environment
// that has a window fails with "unknown time zone", and the failure looks like the
// target refusing.
//
// A test that loads a zone cannot show this: the machine it runs on has the
// database, and the standard library only reaches for its own copy when it does
// not find one. So the import itself is what is checked, the way internal/arch
// checks the layers.
func TestTheWorkerCarriesItsOwnTimeZoneDatabase(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}

	for _, imported := range file.Imports {
		if imported.Path.Value == `"time/tzdata"` {
			return
		}
	}
	t.Fatal(`cmd/worker does not import _ "time/tzdata": in an image with no zone database, ` +
		"a window that names a zone cannot be read and every run against it fails")
}

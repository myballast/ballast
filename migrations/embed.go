// Package migrations holds the SQL files of the schema, and carries them inside
// the binary that applies them.
//
// A deployment image has nothing to read them from at the moment it runs
// them: it is a single static binary with no shell and no checkout. The files
// are the ones the Makefile applies to a development database, and in the same
// order.
package migrations

import "embed"

// FS holds every *.up.sql and *.down.sql file of this directory.
//
//go:embed *.sql
var FS embed.FS

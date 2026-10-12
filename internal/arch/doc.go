// Package arch holds no code. It holds the test that reads the rest of the
// repository and refuses a dependency pointing the wrong way.
//
// The rule it enforces is the first one in CLAUDE.md: imports only ever go
// inward, cmd → adapter → usecase → domain. golangci-lint enforces it too,
// through depguard, and that is deliberate duplication — a linter is a
// configuration file somebody can relax, skip or forget to run, while this
// fails `make test` on the machine of whoever wrote the import.
package arch

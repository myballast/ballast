// This file keeps web/ out of the Go module. npm packages can ship Go sources
// (flatted does, under node_modules), and `go test ./...` and golangci-lint
// would otherwise pick them up as part of Ballast.
module github.com/youssefmahersi/ballast/web

go 1.23.0

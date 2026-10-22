// Package openapi reads an OpenAPI or Swagger specification into the list of
// endpoints a scenario can be built from.
//
// The file is untrusted input. It is read as data and never followed anywhere:
// a $ref that leaves the file is refused, the number and the depth of the $ref
// that are followed are bounded, and nothing in it is executed or fetched.
package openapi

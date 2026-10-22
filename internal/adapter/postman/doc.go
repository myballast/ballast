// Package postman reads a Postman collection, in its v2.0 or v2.1 format, and
// an optional environment file, into the requests a scenario can be built from.
//
// The files are untrusted input, and they are read as data: nothing in them is
// run, fetched or followed. A collection has no references to other files, and
// the size, the depth of its folders and the number of its requests are bounded.
package postman

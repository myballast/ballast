// Package har reads a HAR file, the recording a browser makes of its requests,
// into the requests a scenario can be built from.
//
// The file is untrusted input, and it is read as data: nothing in it is run,
// fetched or followed. Its size and its number of entries are bounded. The
// responses, which are most of a HAR file, are skipped while reading and never
// kept; the only thing taken from one is its content type, to tell an API call
// from an image.
//
// A browser records its cookies and its own headers with every request. The
// cookies are removed here, with their values, and the values are handed on so
// that the analysis can refuse an import that still holds one.
package har

// Package audit reads the journal: who did what, to which target, with what
// result, and when.
//
// Writing is not here. Each service records its own actions, because only it
// knows what it just did and why — and because the entries that must never be
// missing travel into the transaction that performs the action, which is the
// repository's business rather than this package's.
package audit

// Package apitoken authenticates the credential a pipeline holds instead of a
// session: a bearer token tied to one project.
//
// What it decides is who is asking. What they may do is authz's question, asked
// again on every request against the token's own row, so that revoking a token
// takes effect on the next one.
package apitoken

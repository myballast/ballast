// Package oidc talks to the identity providers people sign in with.
//
// Google speaks OpenID Connect: it hands back a signed ID token, which is
// verified here. GitHub does not: it is plain OAuth2, so the identity is read
// from its API with the access token instead. Both sit behind the same
// interface, declared by the auth use case, and neither keeps a token once the
// identity has been read.
package oidc

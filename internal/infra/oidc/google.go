package oidc

import (
	"context"
	"errors"
	"fmt"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// Google's published endpoints. Written out rather than taken from
// golang.org/x/oauth2/google, which would pull in the Google Cloud metadata
// client for the sake of two URLs.
const (
	googleIssuer  = "https://accounts.google.com"
	googleKeysURL = "https://www.googleapis.com/oauth2/v3/certs"
)

var googleEndpoint = oauth2.Endpoint{
	AuthURL:   "https://accounts.google.com/o/oauth2/auth",
	TokenURL:  "https://oauth2.googleapis.com/token",
	AuthStyle: oauth2.AuthStyleInParams,
}

// Google signs people in with their Google account, over OpenID Connect.
type Google struct {
	conf     oauth2.Config
	verifier *gooidc.IDTokenVerifier
}

// NewGoogle returns a provider for the OAuth client with these credentials.
//
// The endpoints and the issuer are Google's published ones rather than
// discovered at start-up, so that the API starts without reaching Google. The
// signing keys are fetched on the first sign-in, and again whenever Google
// rotates them.
func NewGoogle(clientID, clientSecret, redirectURL string) *Google {
	return newGoogle(clientID, clientSecret, redirectURL, googleIssuer, googleKeysURL)
}

func newGoogle(clientID, clientSecret, redirectURL, issuer, keysURL string) *Google {
	return &Google{
		conf: oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Endpoint:     googleEndpoint,
			Scopes:       []string{gooidc.ScopeOpenID, "email", "profile"},
		},
		// The audience check is what stops a token Google issued to some
		// other application from signing anyone in here.
		verifier: gooidc.NewVerifier(
			issuer,
			gooidc.NewRemoteKeySet(context.Background(), keysURL),
			&gooidc.Config{ClientID: clientID},
		),
	}
}

// AuthCodeURL is Google's consent page, with the PKCE challenge.
func (g *Google) AuthCodeURL(state, verifier string) string {
	return g.conf.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
}

// Exchange trades the code for tokens and reads the identity from the ID
// token, after checking its signature, issuer, audience and expiry.
func (g *Google) Exchange(ctx context.Context, code, verifier string) (domain.ExternalIdentity, error) {
	token, err := g.conf.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return domain.ExternalIdentity{}, fmt.Errorf("google: exchange the code: %w", err)
	}

	raw, ok := token.Extra("id_token").(string)
	if !ok || raw == "" {
		return domain.ExternalIdentity{}, errors.New("google: the token response carries no ID token")
	}

	idToken, err := g.verifier.Verify(ctx, raw)
	if err != nil {
		return domain.ExternalIdentity{}, fmt.Errorf("google: verify the ID token: %w", err)
	}

	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return domain.ExternalIdentity{}, fmt.Errorf("google: read the ID token: %w", err)
	}

	// An address Google has not verified proves nothing about who holds it.
	email := claims.Email
	if !claims.EmailVerified {
		email = ""
	}

	identity := domain.ExternalIdentity{
		Subject: "google|" + idToken.Subject,
		Email:   email,
		Name:    claims.Name,
	}
	return identity, identity.Validate()
}

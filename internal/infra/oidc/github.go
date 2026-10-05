package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// GitHub signs people in with their GitHub account.
type GitHub struct {
	conf   oauth2.Config
	apiURL string
}

// NewGitHub returns a provider for the OAuth app with these credentials.
// redirectURL must match the callback registered on the app.
func NewGitHub(clientID, clientSecret, redirectURL string) *GitHub {
	return &GitHub{
		conf: oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Endpoint:     github.Endpoint,
			// Read-only profile and the addresses, which is what it takes to
			// find a verified one. Nothing touching repositories.
			Scopes: []string{"read:user", "user:email"},
		},
		apiURL: "https://api.github.com",
	}
}

// AuthCodeURL is GitHub's authorization page, with the PKCE challenge.
func (g *GitHub) AuthCodeURL(state, verifier string) string {
	return g.conf.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
}

// Exchange trades the code for an access token, then reads who it belongs to.
// The token is used for these two calls and dropped.
func (g *GitHub) Exchange(ctx context.Context, code, verifier string) (domain.ExternalIdentity, error) {
	token, err := g.conf.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return domain.ExternalIdentity{}, fmt.Errorf("github: exchange the code: %w", err)
	}
	client := g.conf.Client(ctx, token)

	var user struct {
		ID    int64   `json:"id"`
		Login string  `json:"login"`
		Name  *string `json:"name"`
	}
	if err := g.get(client, "/user", &user); err != nil {
		return domain.ExternalIdentity{}, err
	}
	if user.ID == 0 {
		return domain.ExternalIdentity{}, errors.New("github: the user has no id")
	}

	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := g.get(client, "/user/emails", &emails); err != nil {
		return domain.ExternalIdentity{}, err
	}

	// The primary address if GitHub verified it, otherwise any verified one.
	// An unverified address proves nothing about who holds it.
	var email string
	for _, e := range emails {
		if e.Verified && (e.Primary || email == "") {
			email = e.Email
		}
	}

	name := user.Login
	if user.Name != nil && *user.Name != "" {
		name = *user.Name
	}

	identity := domain.ExternalIdentity{
		// The numeric id, not the login: a login can be renamed, and then
		// claimed by somebody else.
		Subject: "github|" + strconv.FormatInt(user.ID, 10),
		Email:   email,
		Name:    name,
	}
	return identity, identity.Validate()
}

// get reads one JSON document from the API, refusing anything larger than a
// profile has any reason to be.
func (g *GitHub) get(client *http.Client, path string, into any) error {
	req, err := http.NewRequest(http.MethodGet, g.apiURL+path, nil)
	if err != nil {
		return fmt.Errorf("github: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("github: GET %s: %w", path, err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("github: GET %s answered %d", path, res.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(into); err != nil {
		return fmt.Errorf("github: decode %s: %w", path, err)
	}
	return nil
}

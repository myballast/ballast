package config

import (
	"fmt"
	"net/url"
	"time"
)

// API is what the HTTP API reads on top of the shared configuration.
type API struct {
	Config

	// SessionSecret signs the session cookies. Changing it signs everybody out.
	SessionSecret []byte
	SessionTTL    time.Duration

	// PublicURL is where browsers reach this API. The providers' callbacks
	// point at it, and must be registered with them as such.
	PublicURL *url.URL
	// WebURL is the front end, where every sign-in ends.
	WebURL *url.URL

	// A provider without credentials is not offered.
	GitHub OAuthClient
	Google OAuthClient
}

// OAuthClient is an application registered with an identity provider.
type OAuthClient struct {
	ID     string
	Secret string
}

// Enabled reports whether the client is configured.
func (c OAuthClient) Enabled() bool {
	return c.ID != "" && c.Secret != ""
}

// LoadAPI reads the API's configuration.
func LoadAPI() (API, error) {
	common, err := Load()
	if err != nil {
		return API{}, err
	}
	cfg := API{Config: common}

	secret := env("BALLAST_SESSION_SECRET", "")
	if secret == "" {
		return API{}, fmt.Errorf("BALLAST_SESSION_SECRET is not set")
	}
	cfg.SessionSecret = []byte(secret)

	if cfg.SessionTTL, err = duration("BALLAST_SESSION_TTL", 12*time.Hour); err != nil {
		return API{}, err
	}

	if cfg.PublicURL, err = absoluteURL("BALLAST_PUBLIC_URL"); err != nil {
		return API{}, err
	}
	if cfg.WebURL, err = absoluteURL("BALLAST_WEB_URL"); err != nil {
		return API{}, err
	}

	if cfg.GitHub, err = oauthClient("BALLAST_GITHUB"); err != nil {
		return API{}, err
	}
	if cfg.Google, err = oauthClient("BALLAST_GOOGLE"); err != nil {
		return API{}, err
	}

	return cfg, nil
}

// absoluteURL reads a URL that must name a scheme and a host. There is no
// default: a sign-in that returns to the wrong place fails far from the cause.
func absoluteURL(key string) (*url.URL, error) {
	raw := env(key, "")
	if raw == "" {
		return nil, fmt.Errorf("%s is not set", key)
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("%s must be an absolute URL, got %q", key, raw)
	}
	return parsed, nil
}

// oauthClient reads <prefix>_CLIENT_ID and <prefix>_CLIENT_SECRET. Both or
// neither: half a client is a typo, not a choice.
func oauthClient(prefix string) (OAuthClient, error) {
	c := OAuthClient{ID: env(prefix+"_CLIENT_ID", ""), Secret: env(prefix+"_CLIENT_SECRET", "")}
	if (c.ID == "") != (c.Secret == "") {
		return OAuthClient{}, fmt.Errorf("%s_CLIENT_ID and %s_CLIENT_SECRET must be set together", prefix, prefix)
	}
	return c, nil
}

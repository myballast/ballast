package config

import (
	"bytes"
	"errors"
	"net"
	"net/url"

	"github.com/youssefmahersi/ballast/internal/infra/crypto"
)

// The secrets the Makefile hands to a developer's machine. They are in a public
// repository, so anybody holds them: whatever they protect on a server is not
// protected. A test keeps these in step with the Makefile.
const (
	developmentSecretKey     = "YmFsbGFzdC1kZXZlbG9wbWVudC1rZXktZG8tbm90LXU=" //nolint:gosec // public on purpose: it is what is refused
	developmentSessionSecret = "development-only-session-secret-do-not-deploy"
)

// isLocal reports whether a host is this machine.
func isLocal(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// refuseDevelopmentKey stops a process that is not on a developer's machine from
// encrypting secrets with the key everybody has.
//
// "Not on a developer's machine" is read from the database: a worker has no
// public address, but every process has a database, and the local one is the
// loopback. An address that cannot be read is not taken for local.
func refuseDevelopmentKey(databaseURL string, key []byte) error {
	known, err := crypto.KeyFromBase64(developmentSecretKey)
	if err != nil || !bytes.Equal(key, known) {
		return nil
	}

	if parsed, err := url.Parse(databaseURL); err == nil && isLocal(parsed.Hostname()) {
		return nil
	}

	return errors.New("BALLAST_SECRET_KEY is the development key, and the database is not on this machine. " +
		"Every environment has its own: generate one with `openssl rand -base64 32`. " +
		"Changing it makes the secrets already stored unreadable")
}

// refuseDevelopmentSetup applies the same rule to the API's own settings: an
// address that is not this machine's must be https, since the session cookie is
// Secure and a sign-in that returns to http never gets one, and it must not sign
// sessions with the development secret.
func refuseDevelopmentSetup(public, web *url.URL, sessionSecret string) error {
	if isLocal(public.Hostname()) {
		return nil
	}

	if public.Scheme != "https" {
		return errors.New("BALLAST_PUBLIC_URL must be https: the session cookie is Secure, and a browser keeps it only over https")
	}
	if web.Scheme != "https" && !isLocal(web.Hostname()) {
		return errors.New("BALLAST_WEB_URL must be https: the session cookie is Secure, and a browser keeps it only over https")
	}
	if sessionSecret == developmentSessionSecret {
		return errors.New("BALLAST_SESSION_SECRET is the development secret, and this API has a public address. " +
			"Every environment has its own: generate one with `openssl rand -base64 32`. " +
			"Changing it signs everybody out")
	}
	return nil
}

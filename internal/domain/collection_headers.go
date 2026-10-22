package domain

import (
	"net/url"
	"strings"
)

// browserHeaders are the headers a browser sets for itself. They describe the
// browser, not the call, and a load generator sends its own. Cookie and
// Set-Cookie are not here: they are removed with their values kept for the guard.
//
// These helpers are shared by the readers of the formats a browser or a terminal
// produces — HAR and cURL — so that they cannot disagree on what to remove.
var browserHeaders = map[string]bool{
	"user-agent": true, "accept-encoding": true, "accept-language": true,
	"referer": true, "origin": true, "connection": true, "host": true,
	"content-length": true, "upgrade-insecure-requests": true, "dnt": true,
	"pragma": true, "cache-control": true, "priority": true, "te": true,
	"if-none-match": true, "if-modified-since": true, "if-match": true,
	"if-unmodified-since": true, "if-range": true, "range": true,
	"x-client-data": true,
}

// IsBrowserHeader reports whether a header is the browser's own: HTTP/2 pseudo
// headers (:authority), Sec-* and the list above.
func IsBrowserHeader(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	return strings.HasPrefix(lower, ":") || strings.HasPrefix(lower, "sec-") || browserHeaders[lower]
}

// IsCookieHeader reports whether a header carries cookies.
func IsCookieHeader(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	return lower == "cookie" || lower == "set-cookie"
}

// CookieValues lists the values of a Cookie or Set-Cookie header: "a=1; b=2"
// gives "1" and "2". Whatever follows a Set-Cookie's first pair is attributes
// (Path, Expires) and is not a secret, but taking it too only makes the guard
// stricter.
func CookieValues(header string) []string {
	var values []string
	for _, pair := range strings.Split(header, ";") {
		_, value, found := strings.Cut(pair, "=")
		value = strings.Trim(strings.TrimSpace(value), `"`)
		if found && value != "" {
			values = append(values, value)
		}
	}
	return values
}

// SplitAddress returns the host an address names, in lower case, and its path
// without the query, which is what a person recognises a request by. An address
// without a host gives an empty host and the text up to the query.
func SplitAddress(raw string) (host, path string) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		withoutQuery, _, _ := strings.Cut(raw, "?")
		return "", withoutQuery
	}
	path = parsed.EscapedPath()
	if path == "" {
		path = "/"
	}
	return strings.ToLower(parsed.Host), path
}

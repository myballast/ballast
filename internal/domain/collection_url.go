package domain

import (
	"net/url"
	"regexp"
	"strings"
)

// splitURL is a collection's address cut into the part that names a host, the
// path, and the query. Only what a scenario needs: the host is dropped, because
// a scenario sends its load to the environment's base URL and to nothing else.
type splitURL struct {
	// host is how the address names its host: a {{variable}}, or the text
	// between the scheme and the first slash. Empty when the address has none.
	host string
	// hasCredentials is true when the host part carried a user and a password.
	// They are dropped, and the caller says so.
	hasCredentials bool
	path           string
	query          []QueryParam
	// droppedParams are query parameters whose name is not plain text.
	droppedParams int
}

var (
	leadingVariable = regexp.MustCompile(`^\{\{\s*([^}]+?)\s*\}\}`)
	schemePrefix    = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*://`)
)

// splitCollectionURL cuts an address as a collection writes it.
func splitCollectionURL(raw string) splitURL {
	rest := strings.TrimSpace(raw)

	// The fragment is never sent to a server.
	if hash := strings.Index(rest, "#"); hash >= 0 {
		rest = rest[:hash]
	}

	result := splitURL{}
	rest = result.takeHost(rest)

	pathPart, queryPart, _ := strings.Cut(rest, "?")
	result.path = pathOf(pathPart)
	result.query, result.droppedParams = queryOf(queryPart)

	return result
}

// takeHost removes the host from the front of an address and records it.
func (s *splitURL) takeHost(rest string) string {
	if schemePrefix.MatchString(rest) {
		afterScheme := schemePrefix.ReplaceAllString(rest, "")
		return s.cutHostAt(afterScheme)
	}

	if match := leadingVariable.FindStringSubmatch(rest); match != nil {
		after := rest[len(match[0]):]
		// {{baseUrl}}/users: the variable is the host. {{id}} on its own in a
		// path never starts an address, so it only counts when a slash, a query
		// or the end follows.
		if after == "" || strings.HasPrefix(after, "/") || strings.HasPrefix(after, "?") || strings.HasPrefix(after, ":") {
			s.host = "{{" + match[1] + "}}"
			// A port after the variable belongs to the host too.
			if strings.HasPrefix(after, ":") {
				after = s.cutHostAt(after[1:])
				return after
			}
			return after
		}
		return rest
	}

	// localhost:3000/api and api.example.com/x have a host with no scheme.
	if !strings.HasPrefix(rest, "/") {
		firstPart, _, _ := strings.Cut(rest, "/")
		firstPart, _, _ = strings.Cut(firstPart, "?")
		if strings.ContainsAny(firstPart, ".:") && !strings.HasPrefix(firstPart, ":") {
			return s.cutHostAt(rest)
		}
	}
	return rest
}

// cutHostAt takes everything up to the first slash or question mark as the host.
func (s *splitURL) cutHostAt(rest string) string {
	end := len(rest)
	for position, character := range rest {
		if character == '/' || character == '?' {
			end = position
			break
		}
	}

	host := rest[:end]
	if at := strings.LastIndex(host, "@"); at >= 0 {
		s.hasCredentials = true
		host = host[at+1:]
	}
	s.host = strings.ToLower(host)
	return rest[end:]
}

// pathOf turns a collection's path into a path template: :id and {{id}} become
// {id}, and an empty path is the root.
func pathOf(path string) string {
	if path == "" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	segments := strings.Split(path, "/")
	for position, segment := range segments {
		if strings.HasPrefix(segment, ":") && len(segment) > 1 {
			segments[position] = "{" + segment[1:] + "}"
		}
	}
	return strings.Join(segments, "/")
}

// queryOf cuts a query string into its parameters, decoding what is plain text
// so that the generator can encode it once. A name that is not plain text — a
// {{variable}} in a name — cannot be one, and is counted as dropped.
func queryOf(query string) ([]QueryParam, int) {
	if query == "" {
		return nil, 0
	}

	var params []QueryParam
	dropped := 0

	for _, pair := range strings.Split(query, "&") {
		if pair == "" {
			continue
		}
		name, value, _ := strings.Cut(pair, "=")

		if strings.Contains(name, "{{") {
			dropped++
			continue
		}
		params = append(params, QueryParam{Name: decodeQueryText(name), Value: decodeQueryText(value)})
	}
	return params, dropped
}

// decodeQueryText undoes percent-encoding on text that holds no {{variable}},
// and leaves anything else as it is.
func decodeQueryText(text string) string {
	if strings.Contains(text, "{{") {
		return text
	}
	decoded, err := url.QueryUnescape(text)
	if err != nil {
		return text
	}
	return decoded
}

package postman

import (
	"fmt"
	"strings"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// walker goes through the items of a collection depth first, the order a person
// runs them in, and numbers the requests as it goes.
type walker struct {
	requests []domain.CollectionRequest
	findings []domain.ImportFinding
}

// collectionEvents reports the scripts of the collection itself: they apply to
// every request, and Ballast runs none of them.
func (w *walker) collectionEvents(document map[string]any) {
	for _, script := range scriptsOf(document) {
		w.findings = append(w.findings, domain.ImportFinding{Message: scriptFinding("The collection", script)})
	}
}

func scriptFinding(subject string, script domain.CollectionScript) string {
	kind := "tests"
	if script.Kind == "prerequest" {
		kind = "a pre-request script"
	}
	return fmt.Sprintf("%s has %s of %d lines, which applies to its requests and is not converted.", subject, kind, script.Lines)
}

// items walks a list of items: a folder is walked into, a request is kept.
func (w *walker) items(items []any, folders []string, inherited *domain.CollectionAuth, depth int) error {
	if depth > domain.MaxCollectionDepth {
		return fmt.Errorf("%w: folders are nested more than %d deep", domain.ErrInvalidCollection, domain.MaxCollectionDepth)
	}

	for _, entry := range items {
		item := asObject(entry)
		if item == nil {
			continue
		}
		name := shortened(stringAt(item, "name"), 200)

		if nested, isFolder := item["item"].([]any); isFolder {
			if err := w.folder(item, nested, name, folders, inherited, depth); err != nil {
				return err
			}
			continue
		}

		if _, isRequest := item["request"]; isRequest {
			if err := w.request(item, name, folders, inherited); err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *walker) folder(
	item map[string]any, nested []any, name string, folders []string, inherited *domain.CollectionAuth, depth int,
) error {
	for _, script := range scriptsOf(item) {
		w.findings = append(w.findings, domain.ImportFinding{
			Request: name, Message: scriptFinding("The folder "+name, script),
		})
	}

	auth := inherited
	if own := authOf(item); own != nil {
		auth = own
	}

	inside := append(append([]string{}, folders...), name)
	return w.items(nested, inside, auth, depth+1)
}

func (w *walker) request(item map[string]any, name string, folders []string, inherited *domain.CollectionAuth) error {
	if len(w.requests) >= domain.MaxCollectionRequests {
		return fmt.Errorf("%w: the collection holds more than %d requests", domain.ErrInvalidCollection, domain.MaxCollectionRequests)
	}

	request := requestOf(item["request"])
	if name == "" {
		name = fmt.Sprintf("Request %d", len(w.requests)+1)
	}

	auth := inherited
	if own := authOf(request); own != nil {
		auth = own
	}
	// A request that says noauth has none, whatever its folder says.
	if auth != nil && strings.EqualFold(auth.Type, "noauth") {
		auth = nil
	}

	w.requests = append(w.requests, domain.CollectionRequest{
		Position:      len(w.requests),
		Folders:       append([]string{}, folders...),
		Name:          name,
		Method:        methodOf(request),
		URL:           urlOf(request),
		PathVariables: pathVariablesOf(request),
		Headers:       headersOf(request),
		Body:          bodyOf(request),
		Auth:          auth,
		Scripts:       scriptsOf(item),
	})
	return nil
}

// requestOf accepts the two forms a request has: an object, and a bare address,
// which is a GET.
func requestOf(value any) map[string]any {
	if address, isText := value.(string); isText {
		return map[string]any{"method": "GET", "url": address}
	}
	return asObject(value)
}

func methodOf(request map[string]any) string {
	method := stringAt(request, "method")
	if method == "" {
		return "GET"
	}
	return method
}

// urlOf reads the address as the collection wrote it: the raw text when there is
// one, and otherwise the pieces put back together.
func urlOf(request map[string]any) string {
	if text, isText := request["url"].(string); isText {
		return text
	}

	address := objectAt(request, "url")
	if raw := stringAt(address, "raw"); raw != "" {
		return raw
	}

	var out strings.Builder
	if protocol := stringAt(address, "protocol"); protocol != "" {
		out.WriteString(protocol + "://")
	}
	out.WriteString(joinPieces(address["host"], "."))
	if path := joinPieces(address["path"], "/"); path != "" {
		out.WriteString("/" + path)
	}

	var query []string
	for _, entry := range arrayAt(address, "query") {
		param := asObject(entry)
		if stringAt(param, "key") == "" || boolAt(param, "disabled") {
			continue
		}
		query = append(query, stringAt(param, "key")+"="+stringAt(param, "value"))
	}
	if len(query) > 0 {
		out.WriteString("?" + strings.Join(query, "&"))
	}
	return out.String()
}

// joinPieces joins the list a structured address keeps its host and path in.
func joinPieces(value any, separator string) string {
	list, _ := value.([]any)
	pieces := make([]string, 0, len(list))
	for _, piece := range list {
		if text, isText := piece.(string); isText {
			pieces = append(pieces, text)
		}
	}
	return strings.Join(pieces, separator)
}

func pathVariablesOf(request map[string]any) map[string]string {
	variables := map[string]string{}
	for _, entry := range arrayAt(objectAt(request, "url"), "variable") {
		variable := asObject(entry)
		if name := stringAt(variable, "key"); name != "" {
			variables[name] = stringAt(variable, "value")
		}
	}
	return variables
}

// headersOf lists the headers that are switched on.
func headersOf(request map[string]any) []domain.CollectionPair {
	var headers []domain.CollectionPair
	for _, entry := range arrayAt(request, "header") {
		header := asObject(entry)
		if stringAt(header, "key") == "" || boolAt(header, "disabled") {
			continue
		}
		headers = append(headers, domain.CollectionPair{Name: stringAt(header, "key"), Value: stringAt(header, "value")})
	}
	return headers
}

func bodyOf(request map[string]any) domain.CollectionBody {
	body := objectAt(request, "body")
	mode := stringAt(body, "mode")

	result := domain.CollectionBody{Mode: mode}
	switch mode {
	case "raw":
		result.Raw = stringAt(body, "raw")
		result.Language = stringAt(objectAt(objectAt(body, "options"), "raw"), "language")

	case "urlencoded":
		for _, entry := range arrayAt(body, "urlencoded") {
			pair := asObject(entry)
			if stringAt(pair, "key") == "" || boolAt(pair, "disabled") {
				continue
			}
			result.Pairs = append(result.Pairs, domain.CollectionPair{Name: stringAt(pair, "key"), Value: stringAt(pair, "value")})
		}
	}
	return result
}

// authOf reads an auth block into its type and the parameters of that type.
func authOf(holder map[string]any) *domain.CollectionAuth {
	auth := objectAt(holder, "auth")
	kind := stringAt(auth, "type")
	if kind == "" {
		return nil
	}

	params := map[string]string{}
	for _, entry := range arrayAt(auth, kind) {
		param := asObject(entry)
		if name := stringAt(param, "key"); name != "" {
			params[name] = stringAt(param, "value")
		}
	}
	return &domain.CollectionAuth{Type: kind, Params: params}
}

// scriptsOf lists the scripts attached to an item. Only the kind and the number
// of lines are kept: Ballast does not run them.
func scriptsOf(holder map[string]any) []domain.CollectionScript {
	var scripts []domain.CollectionScript

	for _, entry := range arrayAt(holder, "event") {
		event := asObject(entry)
		kind := stringAt(event, "listen")
		lines := len(arrayAt(objectAt(event, "script"), "exec"))
		if kind == "" || lines == 0 {
			continue
		}
		scripts = append(scripts, domain.CollectionScript{Kind: kind, Lines: lines})
	}
	return scripts
}

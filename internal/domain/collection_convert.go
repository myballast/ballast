package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// CollectionAnalysis is what an import makes of the requests somebody chose: the
// nodes they become, the variables those nodes need, the secrets to create, and
// everything that could not be carried over.
//
// It is the one result behind the three things an import does — show what was
// found, build a scenario, fill a catalog — so that they cannot disagree.
type CollectionAnalysis struct {
	// Requests are the chosen requests, in the order they were chosen.
	Requests []AnalysedRequest
	// Variables are the set_variable nodes that open the scenario: the values the
	// collection gave to the variables the requests use.
	Variables []Node
	// Secrets are the environment keys to create, with no value.
	Secrets []DetectedSecret
	// Findings are everything left out or changed, each tied to its request.
	Findings []ImportFinding
	// Duplicates are requests a catalog cannot keep because an earlier one has
	// the same method and path.
	Duplicates []ImportFinding

	// guarded are the secret values the file held, which no output may contain.
	guarded []string
}

// AnalysedRequest is one chosen request.
type AnalysedRequest struct {
	Position int
	Name     string
	// Node is nil when the request was left out, and Findings says why.
	Node *Node
	// Setters run just before the request, to give a path variable the value the
	// collection gave it.
	Setters  []Node
	Findings []ImportFinding
}

// collectionHeadersLeftOut are headers a client sets for itself: Ballast sends
// its own Host and works out the length.
var collectionHeadersLeftOut = map[string]bool{"host": true, "content-length": true}

var (
	schemeWord       = regexp.MustCompile(`(?i)^(bearer|basic|token|digest)\s+`)
	schemeOnly       = regexp.MustCompile(`(?i)^\s*(bearer|basic|token|digest)?\s*$`)
	contentTypeNames = map[string]string{"json": "application/json", "xml": "application/xml", "text": "text/plain"}
)

// analyser carries what one analysis learns as it goes.
type analyser struct {
	book *secretBook
	// values are the non-secret variables, by the name a graph will use.
	values map[string]string
	// order is the variables' names, as the file lists them.
	order []string
	// hostVars are the variables used as the address, which BASE_URL replaces.
	hostVars map[string]bool
	// pathDefined are names given a value by a request's path variables.
	pathDefined map[string]bool
	// used are the variables the converted requests read.
	used map[string]bool
	// lastSet is the value each variable was last given, so that a setter is
	// written only when it changes something.
	lastSet map[string]string

	host    string
	hostSet bool

	// reported keeps a finding from being said twice.
	reported map[string]bool
	current  string
	local    []ImportFinding
	global   []ImportFinding
}

// AnalyseCollection converts the chosen requests, named by position. A choice of
// nothing, or of a position the collection does not have, is refused.
func AnalyseCollection(collection ImportedCollection, chosen []int) (CollectionAnalysis, error) {
	if len(chosen) == 0 {
		return CollectionAnalysis{}, fmt.Errorf("%w: no request was chosen", ErrInvalidCollection)
	}

	byPosition := make(map[int]CollectionRequest, len(collection.Requests))
	for _, request := range collection.Requests {
		byPosition[request.Position] = request
	}

	a := newAnalyser(collection)
	analysis := CollectionAnalysis{}
	alreadyChosen := map[int]bool{}

	for _, position := range chosen {
		if alreadyChosen[position] {
			continue
		}
		alreadyChosen[position] = true

		request, found := byPosition[position]
		if !found {
			return CollectionAnalysis{}, fmt.Errorf("%w: the collection has no request %d", ErrInvalidCollection, position)
		}
		analysis.Requests = append(analysis.Requests, a.convert(request))
	}

	analysis.Variables = a.variableNodes()
	analysis.Secrets = a.book.secrets(a.used)
	analysis.guarded = a.book.values
	analysis.Duplicates = duplicatesIn(analysis.Requests)

	analysis.Findings = append(analysis.Findings, collection.Findings...)
	analysis.Findings = append(analysis.Findings, a.book.findings...)
	for _, request := range analysis.Requests {
		analysis.Findings = append(analysis.Findings, request.Findings...)
	}
	analysis.Findings = append(analysis.Findings, a.global...)

	return analysis, nil
}

func newAnalyser(collection ImportedCollection) *analyser {
	a := &analyser{
		book:        newSecretBook(),
		values:      map[string]string{},
		hostVars:    map[string]bool{},
		pathDefined: map[string]bool{},
		used:        map[string]bool{},
		lastSet:     map[string]string{},
		reported:    map[string]bool{},
	}

	// Every variable is classified before any request is read, in the order the
	// file lists them, so that the keys they get do not depend on which request
	// happens to use them first.
	for _, variable := range collection.Variables {
		name := safeVariableName(variable.Name)
		a.order = append(a.order, name)

		if variable.Secret || looksLikeASecretName(variable.Name) || looksLikeASecretValue(variable.Value) {
			a.book.markVariable(name)
			if !strings.Contains(variable.Value, "{{") {
				a.book.remember(variable.Value)
			}
			continue
		}
		a.values[name] = variable.Value
		a.lastSet[name] = variable.Value
	}
	return a
}

// note records a finding about the request being converted, once.
func (a *analyser) note(format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	key := a.current + "\x00" + message
	if a.reported[key] {
		return
	}
	a.reported[key] = true
	a.local = append(a.local, ImportFinding{Request: a.current, Message: message})
}

// noteForCollection records a finding about the collection as a whole, once.
func (a *analyser) noteForCollection(format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	if a.reported["\x00"+message] {
		return
	}
	a.reported["\x00"+message] = true
	a.global = append(a.global, ImportFinding{Message: message})
}

// convert turns one request into a node, or leaves it out and says why.
func (a *analyser) convert(request CollectionRequest) AnalysedRequest {
	a.current = request.Name
	a.local = nil
	result := AnalysedRequest{Position: request.Position, Name: request.Name}

	node, setters := a.build(request)
	if node != nil {
		probe := *node
		probe.ID = "probe"
		if err := probe.validate(); err != nil {
			a.note("could not be turned into a request a scenario can make, so it was left out")
			node = nil
			setters = nil
		}
	}

	result.Node = node
	result.Setters = setters
	result.Findings = a.local
	return result
}

// build does the conversion; a nil node means the request was left out.
func (a *analyser) build(request CollectionRequest) (*Node, []Node) {
	method := strings.ToUpper(strings.TrimSpace(request.Method))
	if !httpMethods[method] {
		a.note("uses the method %s, which a scenario cannot make, so it was left out", shorten(method, 20))
		return nil, nil
	}

	parts := splitCollectionURL(request.URL)
	if !a.sameHost(parts.host) {
		a.note("addresses another host (%s), and a scenario sends its load to one target only, so it was left out",
			shorten(parts.host, 80))
		return nil, nil
	}
	if parts.hasCredentials {
		a.note("its address held a user and a password, which were not copied")
	}
	if parts.droppedParams > 0 {
		a.note("%d query parameters whose name is a variable were left out", parts.droppedParams)
	}

	node := &Node{
		Kind:   NodeHTTPRequest,
		Method: method,
		Group:  shorten(strings.Join(request.Folders, " / "), maxNodeLabelLength),
		Label:  shorten(request.Name, maxNodeLabelLength),
	}

	path, ok := a.templatePath(parts.path)
	if !ok {
		a.note("its path could not be read, so it was left out")
		return nil, nil
	}
	node.Path = path
	node.Query = a.queryParams(parts.query)
	node.Headers = a.headers(request)

	a.applyAuth(request.Auth, node)
	a.applyBody(request.Body, node)
	a.noteScripts(request.Scripts)

	if len(node.Headers) == 0 {
		node.Headers = nil
	}
	return node, a.pathSetters(path, request.PathVariables)
}

// sameHost says whether a request addresses the host the first one did. A
// request with no host in its address always does.
func (a *analyser) sameHost(host string) bool {
	if host == "" {
		return true
	}

	resolved := host
	if strings.HasPrefix(host, "{{") {
		name := safeVariableName(strings.TrimSpace(strings.Trim(host, "{}")))
		a.hostVars[name] = true
		if value, defined := a.values[name]; defined && strings.TrimSpace(value) != "" {
			resolved = splitCollectionURL(value).host
			if resolved == "" {
				resolved = strings.ToLower(strings.TrimSpace(value))
			}
		}
	}

	if !a.hostSet {
		a.host, a.hostSet = resolved, true
		return true
	}
	return resolved == a.host
}

// template rewrites the {{names}} in a text so that each is a usable variable
// name, and notes every one it could not keep: a dynamic variable such as
// {{$guid}} has no equivalent and becomes empty. It reports false for a text
// that opens a {{ it never closes.
func (a *analyser) template(text string) (string, bool) {
	var out strings.Builder
	rest := text

	for {
		open := strings.Index(rest, "{{")
		if open < 0 {
			out.WriteString(rest)
			return out.String(), true
		}
		closing := strings.Index(rest[open:], "}}")
		if closing < 0 {
			return "", false
		}
		closing += open

		out.WriteString(rest[:open])
		name := strings.TrimSpace(rest[open+2 : closing])

		switch {
		case strings.HasPrefix(name, "$"):
			a.note("uses the dynamic variable {{%s}}, which has no equivalent, so it is empty", shorten(name, 40))
		case name == "":
			// An empty placeholder says nothing.
		default:
			safe := safeVariableName(name)
			a.used[safe] = true
			out.WriteString("{{" + safe + "}}")
		}
		rest = rest[closing+2:]
	}
}

// templatePath rewrites a path's {{names}} as {names}, the way a path is written
// in a graph.
func (a *analyser) templatePath(path string) (string, bool) {
	var out strings.Builder
	rest := path

	for {
		open := strings.Index(rest, "{{")
		if open < 0 {
			out.WriteString(rest)
			break
		}
		closing := strings.Index(rest[open:], "}}")
		if closing < 0 {
			return "", false
		}
		closing += open

		out.WriteString(rest[:open])
		name := strings.TrimSpace(rest[open+2 : closing])
		switch {
		case strings.HasPrefix(name, "$"):
			a.note("uses the dynamic variable {{%s}} in its path, which has no equivalent, so it is empty", shorten(name, 40))
		case name != "":
			out.WriteString("{" + safeVariableName(name) + "}")
		}
		rest = rest[closing+2:]
	}

	result := out.String()
	for _, name := range placeholderNames(result) {
		a.used[name] = true
	}
	return result, true
}

// placeholderNames lists the names between single braces in a path.
func placeholderNames(path string) []string {
	var names []string
	rest := path
	for {
		open := strings.Index(rest, "{")
		if open < 0 {
			return names
		}
		closing := strings.Index(rest[open:], "}")
		if closing < 0 {
			return names
		}
		closing += open
		names = append(names, rest[open+1:closing])
		rest = rest[closing+1:]
	}
}

// queryParams converts a query string's parameters, taking a secret out of any
// whose name or value says it is one.
func (a *analyser) queryParams(params []QueryParam) []QueryParam {
	var kept []QueryParam

	for _, param := range params {
		value, ok := a.template(param.Value)
		if !ok {
			a.note("the query parameter %s could not be read, so it was left out", shorten(param.Name, 40))
			continue
		}
		kept = append(kept, QueryParam{Name: param.Name, Value: a.secured(param.Name, param.Value, value, false)})
	}
	return kept
}

// secured returns the value to keep for something called name: a reference to a
// secret variable when the value is a literal secret, and the value as it is
// when it is not. original is the text before placeholders were rewritten.
func (a *analyser) secured(name, original, rewritten string, force bool) string {
	if rewritten == "" {
		return rewritten
	}
	isLiteral := !strings.Contains(original, "{{")
	secretName := force || looksLikeASecretName(name)

	if isLiteral && (secretName || looksLikeASecretValue(rewritten)) {
		variable := a.book.variableForLiteral(name, rewritten)
		a.used[variable] = true
		return "{{" + variable + "}}"
	}

	if secretName {
		// A variable read into something called token is a secret, whatever
		// it was named.
		for _, variable := range referencedVariables(rewritten) {
			a.book.markVariable(variable)
		}
	}
	return rewritten
}

// referencedVariables lists the names between double braces in a text.
func referencedVariables(text string) []string {
	parts, err := ParseTemplate(text)
	if err != nil {
		return nil
	}

	var names []string
	for _, part := range parts {
		if part.Variable != "" {
			names = append(names, part.Variable)
		}
	}
	return names
}

// headers converts a request's headers, taking secrets out of the credentials.
func (a *analyser) headers(request CollectionRequest) map[string]string {
	headers := map[string]string{}

	for _, pair := range request.Headers {
		name := strings.TrimSpace(pair.Name)
		if name == "" || collectionHeadersLeftOut[strings.ToLower(name)] {
			continue
		}

		headers[name] = a.headerValue(name, pair.Value, IsCredentialHeader(name))
	}
	return headers
}

// headerValue converts one header's value. A credential keeps its scheme word
// and loses whatever follows it, which becomes a secret variable.
func (a *analyser) headerValue(name, value string, credential bool) string {
	rewritten, ok := a.template(value)
	if !ok {
		a.note("the header %s could not be read, so its value is empty", shorten(name, 40))
		return ""
	}
	if rewritten == "" {
		return ""
	}

	credential = credential || looksLikeASecretName(name)
	headerVariable := strings.ToLower(safeVariableName(name))

	if !credential {
		return a.secured(headerVariable, value, rewritten, false)
	}

	parts, err := ParseTemplate(rewritten)
	if err != nil {
		return ""
	}

	// Anything read into a credential is a secret.
	for _, part := range parts {
		if part.Variable != "" {
			a.book.markVariable(part.Variable)
		}
	}

	var out strings.Builder
	for _, part := range parts {
		switch {
		case part.Variable != "":
			out.WriteString("{{" + part.Variable + "}}")
		case schemeOnly.MatchString(part.Text):
			out.WriteString(part.Text)
		default:
			out.WriteString(a.literalCredential(headerVariable, part.Text))
		}
	}
	return out.String()
}

// literalCredential keeps a scheme word such as "Bearer " and turns what follows
// it into a secret variable.
func (a *analyser) literalCredential(name, text string) string {
	scheme := ""
	secret := text
	if found := schemeWord.FindString(text); found != "" {
		scheme = found
		secret = strings.TrimPrefix(text, found)
	}

	variable := a.book.variableForLiteral(name, secret)
	a.used[variable] = true
	return scheme + "{{" + variable + "}}"
}

// applyAuth turns the authentication a request ends up with into a header.
func (a *analyser) applyAuth(auth *CollectionAuth, node *Node) {
	if auth == nil {
		return
	}
	kind := strings.ToLower(auth.Type)

	switch kind {
	case "", "noauth":
		return

	case "bearer":
		if hasHeader(node.Headers, "Authorization") || auth.Params["token"] == "" {
			return
		}
		node.Headers["Authorization"] = a.headerValue("Authorization", "Bearer "+auth.Params["token"], true)

	case "apikey":
		a.applyAPIKey(auth, node)

	default:
		a.note("uses %s authentication, which is not converted: no credential was copied", shorten(kind, 30))
	}
}

func (a *analyser) applyAPIKey(auth *CollectionAuth, node *Node) {
	key := strings.TrimSpace(auth.Params["key"])
	value := auth.Params["value"]
	if key == "" || value == "" {
		return
	}

	if strings.EqualFold(auth.Params["in"], "query") {
		rewritten, ok := a.template(value)
		if ok {
			node.Query = append(node.Query, QueryParam{Name: key, Value: a.secured(key, value, rewritten, true)})
		}
		return
	}

	if !hasHeader(node.Headers, key) {
		node.Headers[key] = a.headerValue(key, value, true)
	}
}

func hasHeader(headers map[string]string, name string) bool {
	for existing := range headers {
		if strings.EqualFold(existing, name) {
			return true
		}
	}
	return false
}

// applyBody converts a body, or says why it cannot.
func (a *analyser) applyBody(body CollectionBody, node *Node) {
	switch body.Mode {
	case "":
		return
	case "formdata", "file", "graphql":
		a.note("has a %s body, which is not converted", body.Mode)
		return
	}

	text := ""
	contentType := ""

	switch body.Mode {
	case "raw":
		var ok bool
		text, ok = a.rawBody(body)
		if !ok {
			return
		}
		contentType = contentTypeFor(body)

	case "urlencoded":
		text = a.formBody(body.Pairs)
		contentType = "application/x-www-form-urlencoded"

	default:
		a.note("has a %s body, which is not converted", shorten(body.Mode, 30))
		return
	}

	if len(text) > MaxImportedTextLength {
		a.note("its body is longer than %d characters, so the body is empty", MaxImportedTextLength)
		return
	}
	node.Body = text

	if contentType != "" && !hasHeader(node.Headers, "Content-Type") {
		node.Headers["Content-Type"] = contentType
	}
}

func contentTypeFor(body CollectionBody) string {
	if known, ok := contentTypeNames[strings.ToLower(body.Language)]; ok {
		return known
	}
	trimmed := strings.TrimSpace(body.Raw)
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return "application/json"
	}
	return ""
}

// rawBody converts a raw body. A JSON body is searched for secrets by key and by
// value, and a secret that cannot be taken out cleanly drops the whole body:
// dropping is safe, and guessing is not.
func (a *analyser) rawBody(body CollectionBody) (string, bool) {
	raw := body.Raw
	if raw == "" {
		return "", false
	}

	if contentTypeFor(body) == "application/json" {
		scrubbed, clean := a.scrubJSON(raw)
		if !clean {
			a.note("its body holds a secret that could not be taken out safely, so the body is empty")
			return "", false
		}
		raw = scrubbed
	}

	rewritten, ok := a.template(raw)
	if !ok {
		a.note("its body could not be read, so the body is empty")
		return "", false
	}
	return rewritten, true
}

// scrubJSON replaces each literal secret of a JSON body with a {{variable}}.
func (a *analyser) scrubJSON(raw string) (string, bool) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()

	var parsed any
	if err := decoder.Decode(&parsed); err != nil {
		// Not JSON after all: the text goes on as text.
		return raw, true
	}

	found := map[string]string{}
	a.collectSecrets(parsed, "", found)

	result := raw
	names := make([]string, 0, len(found))
	for key := range found {
		names = append(names, key)
	}
	sort.Strings(names)

	for _, key := range names {
		literal := found[key]
		variable := a.book.variableForLiteral(lastNameOf(key), literal)
		a.used[variable] = true
		result = replaceJSONValue(result, lastNameOf(key), literal, variable)
	}

	// Whatever the replacement did, none of what was found may be left.
	for _, literal := range found {
		if strings.Contains(result, literal) {
			return "", false
		}
	}
	return result, true
}

// collectSecrets walks a parsed JSON value and records the literal strings that
// are secrets, under a path of keys, so that two keys of the same name in
// different places are both found.
func (a *analyser) collectSecrets(value any, path string, found map[string]string) {
	switch typed := value.(type) {
	case map[string]any:
		for key, inner := range typed {
			a.collectSecrets(inner, path+"\x00"+key, found)
		}
	case []any:
		for position, inner := range typed {
			a.collectSecrets(inner, fmt.Sprintf("%s\x00[%d]", path, position), found)
		}
	case string:
		name := lastNameOf(path)
		if typed == "" || strings.Contains(typed, "{{") {
			return
		}
		if looksLikeASecretName(name) || looksLikeASecretValue(typed) {
			found[path] = typed
		}
	}
}

// lastNameOf is the final key of a path built by collectSecrets.
func lastNameOf(path string) string {
	position := strings.LastIndex(path, "\x00")
	return path[position+1:]
}

// replaceJSONValue replaces "key": "literal" with "key": "{{variable}}" in the
// text of a JSON document, wherever it appears.
func replaceJSONValue(text, key, literal, variable string) string {
	pattern := regexp.MustCompile(`(` + regexp.QuoteMeta(jsonString(key)) + `\s*:\s*)` + regexp.QuoteMeta(jsonString(literal)))
	return pattern.ReplaceAllString(text, `${1}"{{`+variable+`}}"`)
}

// jsonString writes text as a JSON string, without the HTML escaping that would
// make it differ from how a person wrote it.
func jsonString(text string) string {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(text); err != nil {
		return `""`
	}
	return strings.TrimSpace(out.String())
}

// formBody writes an urlencoded body, taking a secret out of any field whose
// name or value says it is one.
func (a *analyser) formBody(pairs []CollectionPair) string {
	var fields []string

	for _, pair := range pairs {
		name := strings.TrimSpace(pair.Name)
		if name == "" {
			continue
		}

		value, ok := a.template(pair.Value)
		if !ok {
			a.note("the field %s could not be read, so it is empty", shorten(name, 40))
			value = ""
		}
		value = a.secured(name, pair.Value, value, false)

		fields = append(fields, url.QueryEscape(name)+"="+escapeAroundPlaceholders(value))
	}
	return strings.Join(fields, "&")
}

// escapeAroundPlaceholders URL-encodes the literal parts of a form value and
// leaves {{variables}} as they are, to be substituted at run time.
func escapeAroundPlaceholders(value string) string {
	parts, err := ParseTemplate(value)
	if err != nil {
		return url.QueryEscape(value)
	}

	var out strings.Builder
	for _, part := range parts {
		if part.Variable != "" {
			out.WriteString("{{" + part.Variable + "}}")
		} else {
			out.WriteString(url.QueryEscape(part.Text))
		}
	}
	return out.String()
}

// noteScripts reports the scripts a request carries: Ballast does not run them.
func (a *analyser) noteScripts(scripts []CollectionScript) {
	for _, script := range scripts {
		switch script.Kind {
		case "prerequest":
			a.note("has a pre-request script of %d lines, which is not converted", script.Lines)
		case "test":
			a.note("has tests of %d lines, which are not converted", script.Lines)
		default:
			a.note("has a %s script of %d lines, which is not converted", shorten(script.Kind, 20), script.Lines)
		}
	}
}

// pathSetters gives each path variable the value the collection gave it, with a
// set_variable placed just before the request, unless the variable already has
// that value.
func (a *analyser) pathSetters(path string, values map[string]string) []Node {
	var setters []Node

	for _, name := range placeholderNames(path) {
		value, given := values[name]
		a.pathDefined[name] = true
		if !given || value == "" {
			continue
		}

		if looksLikeASecretName(name) || looksLikeASecretValue(value) {
			a.book.remember(value)
			a.book.markVariable(name)
			continue
		}

		rewritten, ok := a.template(value)
		if !ok || a.lastSet[name] == rewritten {
			continue
		}
		a.lastSet[name] = rewritten
		setters = append(setters, Node{Kind: NodeSetVariable, Variable: name, Value: rewritten})
	}
	return setters
}

// variableNodes writes the set_variable nodes that open the scenario, for the
// variables the converted requests use.
func (a *analyser) variableNodes() []Node {
	a.closeOverVariableValues()

	names := append([]string{}, a.order...)
	var extra []string
	for variable := range a.book.keyOf {
		if !contains(names, variable) {
			extra = append(extra, variable)
		}
	}
	sort.Strings(extra)
	names = append(names, extra...)

	var nodes []Node
	for _, name := range names {
		if !a.used[name] || a.hostVars[name] {
			continue
		}

		if a.book.isSecretVariable(name) {
			nodes = append(nodes, Node{Kind: NodeSetVariable, Variable: name, FromEnv: a.book.keyOf[name]})
			continue
		}

		value, ok := a.variableValue(name)
		if !ok {
			continue
		}
		nodes = append(nodes, Node{Kind: NodeSetVariable, Variable: name, Value: value})
	}

	a.reportUndefined(names)
	return nodes
}

func contains(list []string, wanted string) bool {
	for _, item := range list {
		if item == wanted {
			return true
		}
	}
	return false
}

// variableValue is a variable's value as a node will hold it.
func (a *analyser) variableValue(name string) (string, bool) {
	a.current = ""
	value, ok := a.template(a.values[name])
	if !ok || len(value) > maxNodeTextLength {
		a.noteForCollection("The value of the variable %s could not be kept, so it is empty.", shorten(name, 40))
		return "", true
	}
	return value, true
}

// closeOverVariableValues marks as used the variables that the values of used
// variables read in turn.
func (a *analyser) closeOverVariableValues() {
	for changed := true; changed; {
		changed = false
		for name := range a.used {
			for _, inner := range referencedVariables(a.values[name]) {
				if !a.used[safeVariableName(inner)] {
					a.used[safeVariableName(inner)] = true
					changed = true
				}
			}
		}
	}
}

// reportUndefined says, once each, which variables the requests read that
// nothing defines.
func (a *analyser) reportUndefined(defined []string) {
	var missing []string
	for name := range a.used {
		if !contains(defined, name) && !a.pathDefined[name] && !a.hostVars[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)

	for _, name := range missing {
		a.noteForCollection("The variable %s is used, but neither the collection nor the environment gives it a value.", shorten(name, 40))
	}
}

// duplicatesIn lists the requests a catalog cannot keep because an earlier one
// has the same method and path.
func duplicatesIn(requests []AnalysedRequest) []ImportFinding {
	seen := map[string]bool{}
	var duplicates []ImportFinding

	for _, request := range requests {
		if request.Node == nil {
			continue
		}
		key := request.Node.Method + " " + request.Node.Path
		if seen[key] {
			duplicates = append(duplicates, ImportFinding{
				Request: request.Name,
				Message: fmt.Sprintf("has the same method and path as an earlier request (%s), so the catalog keeps only the first", key),
			})
			continue
		}
		seen[key] = true
	}
	return duplicates
}

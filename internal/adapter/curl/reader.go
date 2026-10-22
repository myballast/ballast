package curl

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// Reader turns pasted cURL commands into requests. It holds no state.
type Reader struct{}

// NewReader returns a reader.
func NewReader() Reader {
	return Reader{}
}

// addressShape is what a bare word must look like to be taken for an address:
// a host with a dot (or localhost), an optional port and path. An unknown option
// followed by its value would otherwise turn the value into a request.
var addressShape = regexp.MustCompile(`^([A-Za-z0-9_-]+\.)+[A-Za-z0-9_-]+(:\d+)?([/?#].*)?$|^localhost(:\d+)?([/?#].*)?$`)

// reading carries what one pasted text leaves behind as it is read.
type reading struct {
	requests []domain.CollectionRequest
	removed  []string
	variable []domain.CollectionVariable
	// credentials maps each user:password seen to the variable standing for it,
	// so that the same one written twice is one secret.
	credentials map[string]string
	findings    []domain.ImportFinding

	skipped, tails int
}

// Read parses the commands. The second argument is not used: there is no
// environment file, and the signature is the one every format shares.
func (Reader) Read(text, _ []byte) (domain.ImportedCollection, error) {
	if len(text) > domain.MaxCurlBytes {
		return domain.ImportedCollection{}, fmt.Errorf("%w: the text is longer than %d KB",
			domain.ErrInvalidCollection, domain.MaxCurlBytes/1024)
	}

	commands := splitCommands(string(text))
	if len(commands) > domain.MaxCurlCommands {
		return domain.ImportedCollection{}, fmt.Errorf("%w: the text holds more than %d commands",
			domain.ErrInvalidCollection, domain.MaxCurlCommands)
	}

	r := &reading{credentials: map[string]string{}}
	for _, line := range commands {
		r.command(line)
	}
	if len(r.requests) == 0 {
		return domain.ImportedCollection{}, fmt.Errorf("%w: no curl command with an address was found", domain.ErrInvalidCollection)
	}

	if r.skipped > 0 {
		r.findings = append(r.findings, domain.ImportFinding{Message: fmt.Sprintf(
			"%d lines that are not curl commands were skipped. Nothing in them was read.", r.skipped)})
	}
	if r.tails > 0 {
		r.findings = append(r.findings, domain.ImportFinding{Message: fmt.Sprintf(
			"After %d commands a pipe or a redirection was found; what follows it was ignored.", r.tails)})
	}

	return domain.ImportedCollection{
		Name:      "cURL",
		Requests:  r.requests,
		Variables: r.variable,
		Findings:  r.findings,
		Removed:   r.removed,
	}, nil
}

// command reads one command, which may name several addresses.
func (r *reading) command(line command) {
	words := withoutPrefix(line.words)
	if len(words) == 0 || !isCurl(words[0].text) {
		if len(line.words) > 0 {
			r.skipped++
		}
		return
	}
	if line.tail {
		r.tails++
	}

	found, positional, unknown := options(words[1:])
	call := newCall(r)

	for _, name := range unknown {
		call.note("the option %s is not used", name)
	}
	for _, entry := range found {
		call.apply(entry)
	}

	for _, address := range call.addresses(positional) {
		if len(r.requests) >= domain.MaxCurlCommands {
			return
		}
		r.requests = append(r.requests, call.request(len(r.requests), address))
	}
}

// withoutPrefix drops what may stand before curl on a line: sudo, and variables
// set for the command (TOKEN=x curl ...). The variables are not read.
func withoutPrefix(words []word) []word {
	for len(words) > 0 {
		text := words[0].text
		name, _, isAssignment := strings.Cut(text, "=")
		if text == "sudo" || (isAssignment && isIdentifier(name)) {
			words = words[1:]
			continue
		}
		break
	}
	return words
}

func isIdentifier(text string) bool {
	if text == "" {
		return false
	}
	for position, character := range text {
		letter := character == '_' || (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z')
		digit := character >= '0' && character <= '9'
		if !letter && (!digit || position == 0) {
			return false
		}
	}
	return true
}

func isCurl(text string) bool {
	base := text[strings.LastIndex(text, "/")+1:]
	return base == "curl" || base == "curl.exe"
}

// call is what the options of one command add up to.
type call struct {
	owner *reading

	method  string
	headers []domain.CollectionPair
	// data are the pieces of the body, joined with & as curl joins them.
	data    []string
	json    string
	hasJSON bool
	urls    []string

	get, head, form, upload bool
	user                    string
	bearer                  string
	notes                   []string
	expands                 bool
	browserHeaders          int
}

func newCall(owner *reading) *call {
	return &call{owner: owner}
}

func (c *call) note(format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	for _, existing := range c.notes {
		if existing == message {
			return
		}
	}
	c.notes = append(c.notes, message)
}

// apply records the effect of one option.
func (c *call) apply(entry option) {
	if entry.given && entry.value.expands {
		c.expands = true
	}
	value := entry.value.text

	switch entry.name {
	case "request":
		c.method = strings.ToUpper(strings.TrimSpace(value))
	case "header":
		c.header(value)
	case "data", "data-raw", "data-binary":
		c.addData(entry.name, value)
	case "data-urlencode":
		c.data = append(c.data, c.urlencodedPiece(value))
	case "json":
		c.json, c.hasJSON = value, true
	case "url":
		c.urls = append(c.urls, value)
	case "user":
		c.user = value
	case "cookie":
		c.cookie(value)
	case "oauth2-bearer":
		c.bearer = value
	case "get":
		c.get = true
	case "head":
		c.head = true
	case "form":
		c.form = true
		c.note("%s", describe(entry.name))
	case "upload-file":
		c.upload = true
		c.note("%s", describe(entry.name))
	default:
		if _, isReported := reported[entry.name]; isReported {
			c.note("%s", describe(entry.name))
		}
	}
}

// header reads "Name: value". A cookie is removed, with its values given to the
// guard; the browser's own headers are counted.
func (c *call) header(text string) {
	name, value, found := strings.Cut(text, ":")
	name = strings.TrimSpace(name)
	value = strings.TrimSpace(value)

	// "Name;" sends an empty header, and has nothing to carry over.
	if name == "" || !found {
		return
	}

	switch {
	case domain.IsCookieHeader(name):
		c.removeCookie(value)
	case domain.IsBrowserHeader(name):
		c.browserHeaders++
	case value == "":
		// "Name:" asks curl to remove one of its own headers.
	default:
		c.headers = append(c.headers, domain.CollectionPair{Name: name, Value: value})
	}
}

// cookie reads -b. Text with an equals sign is cookies; a bare word is the name
// of a file, which is never opened.
func (c *call) cookie(text string) {
	if !strings.Contains(text, "=") {
		c.note("the option --cookie names a file, which is never opened")
		return
	}
	c.removeCookie(text)
}

func (c *call) removeCookie(text string) {
	c.owner.removed = append(c.owner.removed, domain.CookieValues(text)...)
	c.note("its cookie was removed, and its value was not kept: a session recorded elsewhere means nothing to a load test")
}

// addData records a body piece. A leading @ names a file, except for --data-raw,
// which never does; the file is not read.
func (c *call) addData(kind, text string) {
	if strings.HasPrefix(text, "@") && kind != "data-raw" {
		c.note("the body is read from a file, which is never opened, so the body is empty")
		return
	}
	c.data = append(c.data, text)
}

// urlencodedPiece writes --data-urlencode: name=content encodes the content,
// =content and content encode all of it.
func (c *call) urlencodedPiece(text string) string {
	if strings.Contains(text, "@") && !strings.Contains(text, "=") {
		c.note("the body is read from a file, which is never opened, so the body is empty")
		return ""
	}
	name, content, hasName := strings.Cut(text, "=")
	if !hasName {
		return url.QueryEscape(text)
	}
	if name == "" {
		return url.QueryEscape(content)
	}
	return name + "=" + url.QueryEscape(content)
}

// addresses picks the positional words that are addresses, with those given by
// --url. A word that is neither is reported.
func (c *call) addresses(positional []word) []string {
	list := append([]string{}, c.urls...)

	for _, w := range positional {
		if w.expands {
			c.expands = true
		}
		if strings.Contains(w.text, "://") || addressShape.MatchString(w.text) {
			list = append(list, w.text)
			continue
		}
		c.note("an argument that is not an address was skipped")
	}
	return list
}

// request builds one request for an address.
func (c *call) request(position int, address string) domain.CollectionRequest {
	address = withScheme(address)
	c.guardAddress(address)

	body := c.body()
	method := c.methodFor(body)

	if c.get && body.Mode != "" {
		address = withQuery(address, c.dataText())
		body = rawBody{}
	}

	headers := append([]domain.CollectionPair{}, c.headers...)
	headers = c.withAuthorization(headers)
	if c.hasJSON {
		headers = withHeader(headers, "Content-Type", "application/json")
		headers = withHeader(headers, "Accept", "application/json")
	}

	notes := append([]string{}, c.notes...)
	if c.browserHeaders > 0 {
		notes = append(notes, "the headers a browser sets for itself were left out")
	}
	if c.expands {
		notes = append(notes, "it uses a shell variable or substitution ($NAME, $(...)), which is never expanded: the text stayed as written")
	}

	host, path := domain.SplitAddress(address)
	return domain.CollectionRequest{
		Position: position,
		Name:     shorten(method+" "+path, 200),
		Method:   method,
		URL:      address,
		Headers:  headers,
		Body:     bodyFor(body, headers),
		Host:     host,
		Notes:    notes,
	}
}

// withScheme gives an address curl would have taken as http the scheme it needs
// to be read as an address.
func withScheme(address string) string {
	if strings.Contains(address, "://") || strings.HasPrefix(address, "/") {
		return address
	}
	return "http://" + address
}

// guardAddress hands the password of an address that has one to the guard.
func (c *call) guardAddress(address string) {
	parsed, err := url.Parse(address)
	if err != nil || parsed.User == nil {
		return
	}
	if password, set := parsed.User.Password(); set {
		c.owner.removed = append(c.owner.removed, password)
	}
}

// rawBody is the body as text and the kind of body it is.
type rawBody struct {
	Mode string
	Text string
}

func (c *call) dataText() string {
	var pieces []string
	for _, piece := range c.data {
		if piece != "" {
			pieces = append(pieces, piece)
		}
	}
	return strings.Join(pieces, "&")
}

func (c *call) body() rawBody {
	switch {
	case c.form:
		return rawBody{Mode: "formdata"}
	case c.hasJSON:
		return rawBody{Mode: "raw", Text: c.json}
	}
	if text := c.dataText(); text != "" {
		return rawBody{Mode: "raw", Text: text}
	}
	return rawBody{}
}

// methodFor applies curl's rules: -X wins, -I is HEAD, a body makes a POST
// unless -G sends it in the query, and -T is a PUT.
func (c *call) methodFor(body rawBody) string {
	switch {
	case c.method != "":
		return c.method
	case c.head:
		return "HEAD"
	case c.upload:
		return "PUT"
	case body.Mode != "" && !c.get:
		return "POST"
	}
	return "GET"
}

// withAuthorization adds the header -u and --oauth2-bearer stand for. A header
// written with -H wins, as in curl.
func (c *call) withAuthorization(headers []domain.CollectionPair) []domain.CollectionPair {
	if hasHeader(headers, "Authorization") {
		return headers
	}

	switch {
	case c.bearer != "":
		return append(headers, domain.CollectionPair{Name: "Authorization", Value: "Bearer " + c.bearer})
	case c.user != "":
		variable := c.owner.basicVariable(c.user)
		c.note("the user and password of --user were not copied. Create the secret %s, holding user:password encoded in base64; the request sends it as Authorization: Basic",
			strings.ToUpper(variable))
		return append(headers, domain.CollectionPair{Name: "Authorization", Value: "Basic {{" + variable + "}}"})
	}
	return headers
}

// basicVariable returns the secret variable standing for a user:password, and
// gives the guard everything that could reveal it.
func (r *reading) basicVariable(credentials string) string {
	if variable, seen := r.credentials[credentials]; seen {
		return variable
	}

	variable := "basic_auth"
	if len(r.credentials) > 0 {
		variable = fmt.Sprintf("basic_auth_%d", len(r.credentials)+1)
	}
	r.credentials[credentials] = variable
	r.variable = append(r.variable, domain.CollectionVariable{Name: variable, Secret: true})

	r.removed = append(r.removed, credentials, base64.StdEncoding.EncodeToString([]byte(credentials)))
	if _, password, found := strings.Cut(credentials, ":"); found {
		r.removed = append(r.removed, password)
	}
	return variable
}

func hasHeader(headers []domain.CollectionPair, name string) bool {
	for _, header := range headers {
		if strings.EqualFold(header.Name, name) {
			return true
		}
	}
	return false
}

func withHeader(headers []domain.CollectionPair, name, value string) []domain.CollectionPair {
	if hasHeader(headers, name) {
		return headers
	}
	return append(headers, domain.CollectionPair{Name: name, Value: value})
}

// withQuery appends text to the query of an address.
func withQuery(address, text string) string {
	if text == "" {
		return address
	}
	if strings.Contains(address, "?") {
		return address + "&" + text
	}
	return address + "?" + text
}

// bodyFor says how the analysis should read the body, from the Content-Type the
// command gave. Without one, curl sends a form.
func bodyFor(body rawBody, headers []domain.CollectionPair) domain.CollectionBody {
	switch body.Mode {
	case "":
		return domain.CollectionBody{}
	case "formdata":
		return domain.CollectionBody{Mode: "formdata"}
	}

	contentType := ""
	for _, header := range headers {
		if strings.EqualFold(header.Name, "Content-Type") {
			contentType = strings.ToLower(header.Value)
		}
	}

	switch {
	case strings.Contains(contentType, "json"):
		return domain.CollectionBody{Mode: "raw", Raw: body.Text, Language: "json"}
	case strings.Contains(contentType, "xml"):
		return domain.CollectionBody{Mode: "raw", Raw: body.Text, Language: "xml"}
	case strings.HasPrefix(contentType, "text/"):
		return domain.CollectionBody{Mode: "raw", Raw: body.Text, Language: "text"}
	case contentType == "" || strings.Contains(contentType, "x-www-form-urlencoded"):
		if isForm(body.Text) {
			return domain.CollectionBody{Mode: "urlencoded", Pairs: formFields(body.Text)}
		}
	}
	return domain.CollectionBody{Mode: "raw", Raw: body.Text}
}

// isForm reports whether a text is name=value pairs joined with &.
func isForm(text string) bool {
	for _, piece := range strings.Split(text, "&") {
		if !strings.Contains(piece, "=") {
			return false
		}
	}
	return true
}

func formFields(text string) []domain.CollectionPair {
	var fields []domain.CollectionPair
	for _, piece := range strings.Split(text, "&") {
		name, value, _ := strings.Cut(piece, "=")
		fields = append(fields, domain.CollectionPair{Name: unescaped(name), Value: unescaped(value)})
	}
	return fields
}

func unescaped(text string) string {
	decoded, err := url.QueryUnescape(text)
	if err != nil {
		return text
	}
	return decoded
}

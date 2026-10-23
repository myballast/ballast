package har

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// Reader turns the text of a HAR file into the requests it recorded. It holds no
// state.
type Reader struct{}

// NewReader returns a reader.
func NewReader() Reader {
	return Reader{}
}

// The parts of a HAR file that are read. Everything else, the responses' bodies
// above all, is skipped by the decoder without being kept.
type document struct {
	Log *struct {
		Entries []entry `json:"entries"`
	} `json:"log"`
}

type entry struct {
	ResourceType string `json:"_resourceType"`
	Request      struct {
		Method   string `json:"method"`
		URL      string `json:"url"`
		Headers  []pair `json:"headers"`
		PostData *post  `json:"postData"`
	} `json:"request"`
	Response struct {
		Content struct {
			MimeType string `json:"mimeType"`
		} `json:"content"`
	} `json:"response"`
}

type pair struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type post struct {
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
	Params   []pair `json:"params"`
}

// removals counts what was taken out, to say so once for the whole file instead
// of once per request.
type removals struct {
	cookieRequests  int
	browserRequests int
	values          []string
}

// Read parses a HAR file. The environment argument is not used: a HAR file has
// none, and the signature is the one every format of requests shares.
func (Reader) Read(har, _ []byte) (domain.ImportedCollection, error) {
	if len(har) > domain.MaxHARBytes {
		return domain.ImportedCollection{}, fmt.Errorf("%w: the file is larger than %d MB",
			domain.ErrInvalidCollection, domain.MaxHARBytes/(1024*1024))
	}

	var parsed document
	if err := json.NewDecoder(bytes.NewReader(har)).Decode(&parsed); err != nil {
		return domain.ImportedCollection{}, fmt.Errorf("%w: the file is not valid JSON: %v", domain.ErrInvalidCollection, err)
	}
	if parsed.Log == nil {
		return domain.ImportedCollection{}, fmt.Errorf("%w: the file has no log, so it is not a HAR file", domain.ErrInvalidCollection)
	}
	if len(parsed.Log.Entries) > domain.MaxHAREntries {
		return domain.ImportedCollection{}, fmt.Errorf("%w: the file holds %d requests, and at most %d are read",
			domain.ErrInvalidCollection, len(parsed.Log.Entries), domain.MaxHAREntries)
	}

	imported := domain.ImportedCollection{Name: "HAR", ManyHosts: true}
	taken := &removals{}

	for _, recorded := range parsed.Log.Entries {
		imported.Requests = append(imported.Requests, requestOf(len(imported.Requests), recorded, taken))
	}

	imported.Removed = taken.values
	imported.Findings = taken.findings()
	return imported, nil
}

// findings says, once, what was removed from the requests.
func (r *removals) findings() []domain.ImportFinding {
	var findings []domain.ImportFinding

	if r.cookieRequests > 0 {
		findings = append(findings, domain.ImportFinding{Message: fmt.Sprintf(
			"The cookies of %d requests were removed, and their values were not kept: a session recorded in a browser means nothing to a load test. Send an Authorization header, or log in from the scenario.",
			r.cookieRequests)})
	}
	if r.browserRequests > 0 {
		findings = append(findings, domain.ImportFinding{Message: fmt.Sprintf(
			"The headers a browser sets for itself (User-Agent, Accept-Encoding, Referer, Origin, Sec-*) were left out of %d requests.",
			r.browserRequests)})
	}
	return findings
}

// requestOf converts one recorded entry. position is its rank in the file.
func requestOf(position int, recorded entry, taken *removals) domain.CollectionRequest {
	address := recorded.Request.URL
	method := strings.ToUpper(strings.TrimSpace(recorded.Request.Method))
	host, path := domain.SplitAddress(address)

	return domain.CollectionRequest{
		Position: position,
		Name:     shortened(method+" "+path, 200),
		Method:   method,
		URL:      address,
		Headers:  headersOf(recorded.Request.Headers, taken),
		Body:     bodyOf(recorded.Request.PostData),
		Host:     host,
		Kind:     classify(recorded.ResourceType, recorded.Response.Content.MimeType, method, address),
	}
}

// headersOf keeps the headers that describe the call. Cookies and the browser's
// own headers are counted and left out; a cookie's value goes to the guard.
func headersOf(recorded []pair, taken *removals) []domain.CollectionPair {
	var headers []domain.CollectionPair
	cookie, browser := false, false

	for _, header := range recorded {
		switch {
		case domain.IsCookieHeader(header.Name):
			cookie = true
			taken.values = append(taken.values, domain.CookieValues(header.Value)...)
		case domain.IsBrowserHeader(header.Name):
			browser = true
		default:
			headers = append(headers, domain.CollectionPair{Name: header.Name, Value: header.Value})
		}
	}

	if cookie {
		taken.cookieRequests++
	}
	if browser {
		taken.browserRequests++
	}
	return headers
}

// bodyOf converts what was posted. A form is read from its fields, any other
// content from its text; a multipart body is named and left for the analysis to
// report.
func bodyOf(posted *post) domain.CollectionBody {
	if posted == nil {
		return domain.CollectionBody{}
	}

	mime := strings.ToLower(posted.MimeType)
	switch {
	case strings.Contains(mime, "multipart/form-data"):
		return domain.CollectionBody{Mode: "formdata"}

	case strings.Contains(mime, "x-www-form-urlencoded"), posted.Text == "" && len(posted.Params) > 0:
		return domain.CollectionBody{Mode: "urlencoded", Pairs: formFields(posted)}

	case posted.Text == "":
		return domain.CollectionBody{}
	}

	language := ""
	switch {
	case strings.Contains(mime, "json"):
		language = "json"
	case strings.Contains(mime, "xml"):
		language = "xml"
	case strings.HasPrefix(mime, "text/"):
		language = "text"
	}
	return domain.CollectionBody{Mode: "raw", Raw: posted.Text, Language: language}
}

// formFields reads the fields of a form body, in the order they were sent. A
// browser writes both the text and the decoded fields; the fields are used when
// they exist.
func formFields(posted *post) []domain.CollectionPair {
	if len(posted.Params) > 0 {
		fields := make([]domain.CollectionPair, 0, len(posted.Params))
		for _, field := range posted.Params {
			fields = append(fields, domain.CollectionPair{Name: field.Name, Value: field.Value})
		}
		return fields
	}

	var fields []domain.CollectionPair
	for _, piece := range strings.Split(posted.Text, "&") {
		if piece == "" {
			continue
		}
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

// shortened cuts text to at most limit characters.
func shortened(text string, limit int) string {
	characters := []rune(text)
	if len(characters) <= limit {
		return text
	}
	return string(characters[:limit])
}

package har

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/youssefmahersi/ballast/internal/domain"
)

const (
	sessionCookie = "sess-0123456789abcdefghijkl"
	bearerToken   = "ghp_a_token_in_a_har_0123456789abcdef"
)

// A page that calls its API, loads an image and a script, and sends a preflight.
const recording = `{
  "log": {"version": "1.2", "entries": [
    {"_resourceType": "document",
     "request": {"method": "GET", "url": "https://shop.example.com/", "headers": [{"name": "Cookie", "value": "sid=` + sessionCookie + `"}]},
     "response": {"content": {"mimeType": "text/html", "text": "<html>a very long body that must never be kept</html>"}}},
    {"_resourceType": "xhr",
     "request": {"method": "POST", "url": "https://shop.example.com/api/login?remember=1",
       "headers": [
         {"name": ":authority", "value": "shop.example.com"},
         {"name": "User-Agent", "value": "Mozilla/5.0"},
         {"name": "sec-fetch-mode", "value": "cors"},
         {"name": "Cookie", "value": "sid=` + sessionCookie + `; theme=dark"},
         {"name": "Authorization", "value": "Bearer ` + bearerToken + `"},
         {"name": "Content-Type", "value": "application/json"},
         {"name": "Accept", "value": "application/json"}],
       "postData": {"mimeType": "application/json", "text": "{\"email\":\"a@b.c\",\"password\":\"hunter2hunter2\"}"}},
     "response": {"content": {"mimeType": "application/json", "text": "{\"token\":\"` + bearerToken + `\"}"}}},
    {"_resourceType": "fetch",
     "request": {"method": "GET", "url": "https://shop.example.com/api/orders/42", "headers": []},
     "response": {"content": {"mimeType": "application/json"}}},
    {"_resourceType": "image",
     "request": {"method": "GET", "url": "https://cdn.example.com/logo.png", "headers": []},
     "response": {"content": {"mimeType": "image/png"}}},
    {"_resourceType": "preflight",
     "request": {"method": "OPTIONS", "url": "https://shop.example.com/api/orders/42", "headers": []},
     "response": {"content": {}}},
    {"_resourceType": "xhr",
     "request": {"method": "POST", "url": "https://shop.example.com/api/search", "headers": [],
       "postData": {"mimeType": "application/x-www-form-urlencoded", "text": "q=a%26b&page=2",
         "params": [{"name": "q", "value": "a&b"}, {"name": "page", "value": "2"}]}},
     "response": {"content": {"mimeType": "application/json"}}},
    {"_resourceType": "xhr",
     "request": {"method": "POST", "url": "https://shop.example.com/api/upload", "headers": [],
       "postData": {"mimeType": "multipart/form-data; boundary=x", "text": "ignored"}},
     "response": {"content": {"mimeType": "application/json"}}}
  ]}
}`

func read(t *testing.T, text string) domain.ImportedCollection {
	t.Helper()

	imported, err := NewReader().Read([]byte(text), nil)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	return imported
}

func TestRequestsComeOutInTheOrderRecordedWithTheirHostAndKind(t *testing.T) {
	imported := read(t, recording)

	wanted := []struct{ name, host, kind string }{
		{"GET /", "shop.example.com", "document"},
		{"POST /api/login", "shop.example.com", "api"},
		{"GET /api/orders/42", "shop.example.com", "api"},
		{"GET /logo.png", "cdn.example.com", "image"},
		{"OPTIONS /api/orders/42", "shop.example.com", "other"},
		{"POST /api/search", "shop.example.com", "api"},
		{"POST /api/upload", "shop.example.com", "api"},
	}
	if len(imported.Requests) != len(wanted) {
		t.Fatalf("requests = %d, wanted %d", len(imported.Requests), len(wanted))
	}
	for index, want := range wanted {
		got := imported.Requests[index]
		if got.Position != index || got.Name != want.name || got.Host != want.host || got.Kind != want.kind {
			t.Errorf("request %d = %q on %q kind %q, wanted %+v", index, got.Name, got.Host, got.Kind, want)
		}
	}
	if !imported.ManyHosts {
		t.Error("a HAR file lists requests to several hosts, and must say so")
	}
}

func TestCookiesAndBrowserHeadersAreRemovedAndSaidSo(t *testing.T) {
	imported := read(t, recording)

	login := imported.Requests[1]
	names := map[string]bool{}
	for _, header := range login.Headers {
		names[strings.ToLower(header.Name)] = true
	}
	for _, gone := range []string{"cookie", ":authority", "user-agent", "sec-fetch-mode"} {
		if names[gone] {
			t.Errorf("the header %s was kept", gone)
		}
	}
	for _, kept := range []string{"authorization", "content-type", "accept"} {
		if !names[kept] {
			t.Errorf("the header %s was dropped", kept)
		}
	}

	said := ""
	for _, finding := range imported.Findings {
		said += finding.Message + "\n"
	}
	if !strings.Contains(said, "cookies of 2 requests") || !strings.Contains(said, "browser sets for itself") || !strings.Contains(said, "left out of 1 requests") {
		t.Errorf("findings = %q", said)
	}
	if strings.Contains(said, sessionCookie) {
		t.Errorf("a cookie value is in the findings: %q", said)
	}
}

func TestEveryCookieValueIsHandedToTheGuard(t *testing.T) {
	imported := read(t, recording)

	if !contains(imported.Removed, sessionCookie) || !contains(imported.Removed, "dark") {
		t.Errorf("removed = %v", imported.Removed)
	}
}

func TestBodiesAreReadAsFormJSONOrMultipart(t *testing.T) {
	imported := read(t, recording)

	if body := imported.Requests[1].Body; body.Mode != "raw" || body.Language != "json" || !strings.Contains(body.Raw, "hunter2") {
		t.Errorf("login body = %+v", body)
	}
	form := imported.Requests[5].Body
	if form.Mode != "urlencoded" || len(form.Pairs) != 2 || form.Pairs[0].Name != "q" || form.Pairs[0].Value != "a&b" {
		t.Errorf("form body = %+v", form)
	}
	if body := imported.Requests[6].Body; body.Mode != "formdata" || body.Raw != "" {
		t.Errorf("multipart body = %+v", body)
	}
}

func TestAResponseBodyIsNeverKept(t *testing.T) {
	imported := read(t, recording)

	if strings.Contains(fmt.Sprintf("%+v", imported), "never be kept") {
		t.Error("the text of a response is in what was read")
	}
}

func TestKindsWithoutAResourceTypeComeFromTheContentTypeThenTheAddress(t *testing.T) {
	cases := []struct {
		mime, method, url, want string
	}{
		{"application/json; charset=utf-8", "GET", "https://x.test/a", "api"},
		{"application/vnd.api+json", "GET", "https://x.test/a", "api"},
		{"text/xml", "GET", "https://x.test/a", "api"},
		{"image/svg+xml", "GET", "https://x.test/a.svg", "image"},
		{"text/html", "GET", "https://x.test/", "document"},
		{"application/javascript", "GET", "https://x.test/a.js", "script"},
		{"text/css", "GET", "https://x.test/a.css", "style"},
		{"font/woff2", "GET", "https://x.test/a", "font"},
		{"video/mp4", "GET", "https://x.test/a", "media"},
		{"", "GET", "https://x.test/app.js?v=3", "script"},
		{"", "GET", "https://x.test/photo.JPG", "image"},
		{"", "POST", "https://x.test/collect", "api"},
		{"", "GET", "https://x.test/mystery", "other"},
		{"application/octet-stream", "GET", "https://x.test/data", "other"},
	}
	for _, c := range cases {
		if got := classify("", c.mime, c.method, c.url); got != c.want {
			t.Errorf("classify(%q, %s %s) = %q, wanted %q", c.mime, c.method, c.url, got, c.want)
		}
	}
	if got := classify("Stylesheet", "", "GET", "https://x.test/a"); got != "style" {
		t.Errorf("a resource type is read in any case: %q", got)
	}
	if got := classify("websocket", "", "GET", "wss://x.test/a"); got != "other" {
		t.Errorf("an unlisted resource type = %q", got)
	}
}

func TestFilesThatAreNotHARAreRefused(t *testing.T) {
	cases := map[string]string{
		"not json":   `{"log": `,
		"no log":     `{"entries": []}`,
		"json array": `[]`,
	}
	for name, text := range cases {
		_, err := NewReader().Read([]byte(text), nil)
		if !errors.Is(err, domain.ErrInvalidCollection) {
			t.Errorf("%s: error = %v", name, err)
		}
	}
}

func TestAFileLargerThanTheLimitIsRefused(t *testing.T) {
	big := make([]byte, domain.MaxHARBytes+1)
	_, err := NewReader().Read(big, nil)
	if !errors.Is(err, domain.ErrInvalidCollection) || !strings.Contains(err.Error(), "25 MB") {
		t.Errorf("error = %v", err)
	}
}

func TestMoreEntriesThanTheLimitAreRefused(t *testing.T) {
	entry := `{"request": {"method": "GET", "url": "https://x.test/a", "headers": []}, "response": {"content": {}}}`
	text := `{"log": {"entries": [` + strings.Repeat(entry+",", domain.MaxHAREntries) + entry + `]}}`

	_, err := NewReader().Read([]byte(text), nil)
	if !errors.Is(err, domain.ErrInvalidCollection) || !strings.Contains(err.Error(), "5000") {
		t.Errorf("error = %v", err)
	}
}

// The acceptance criterion: five hundred entries, with response bodies as a real
// recording has, are read and analysed in well under a second.
func TestFiveHundredEntriesAreReadAndAnalysedQuickly(t *testing.T) {
	body := strings.Repeat("x", 20000)
	var entries []string
	for index := 0; index < 500; index++ {
		entries = append(entries, fmt.Sprintf(`{"_resourceType": "xhr",
			"request": {"method": "GET", "url": "https://shop.example.com/api/items/%d?page=%d",
			  "headers": [{"name": "Cookie", "value": "sid=%s"}, {"name": "Authorization", "value": "Bearer %s"}]},
			"response": {"content": {"mimeType": "application/json", "text": %q}}}`,
			index, index, sessionCookie, bearerToken, body))
	}
	text := `{"log": {"entries": [` + strings.Join(entries, ",") + `]}}`

	started := time.Now()
	imported := read(t, text)
	preview, err := imported.Preview()
	elapsed := time.Since(started)

	if err != nil || len(preview.Requests) != 500 {
		t.Fatalf("preview: %d requests, %v", len(preview.Requests), err)
	}
	if elapsed > time.Second {
		t.Errorf("500 entries took %v", elapsed)
	}
}

func TestThePreviewListsRequestsOfEveryHostWhileAnImportKeepsOne(t *testing.T) {
	imported := read(t, recording)

	preview, err := imported.Preview()
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	for _, request := range preview.Requests {
		if request.Name == "GET /logo.png" && request.LeftOut {
			t.Error("the preview left out a request only because its host differs from the first")
		}
	}

	// Choosing the CDN image first makes the CDN the target; the API calls are
	// then reported as addressing another host.
	analysis, err := domain.AnalyseCollection(imported, []int{3, 1})
	if err != nil {
		t.Fatalf("AnalyseCollection: %v", err)
	}
	if analysis.Requests[0].Node == nil || analysis.Requests[1].Node != nil {
		t.Errorf("an import keeps one host: %+v", analysis.Requests)
	}
}

func contains(list []string, wanted string) bool {
	for _, item := range list {
		if item == wanted {
			return true
		}
	}
	return false
}

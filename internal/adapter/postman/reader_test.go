package postman

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/youssefmahersi/ballast/internal/domain"
)

const shopCollection = `{
  "info": {"name": "Shop", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
  "auth": {"type": "bearer", "bearer": [{"key": "token", "value": "{{authToken}}", "type": "string"}]},
  "event": [{"listen": "prerequest", "script": {"exec": ["console.log('a');", "console.log('b');"]}}],
  "variable": [{"key": "baseUrl", "value": "https://shop.example.com"}, {"key": "authToken", "value": "tok-123"}],
  "item": [
    {"name": "Auth", "item": [
      {"name": "Login", "request": {
        "method": "POST",
        "header": [{"key": "Accept", "value": "application/json"}, {"key": "X-Off", "value": "1", "disabled": true}],
        "url": {"raw": "{{baseUrl}}/login", "host": ["{{baseUrl}}"], "path": ["login"]},
        "body": {"mode": "raw", "raw": "{\"email\":\"a@b.c\"}", "options": {"raw": {"language": "json"}}},
        "auth": {"type": "noauth"}
      }, "event": [{"listen": "test", "script": {"exec": ["pm.test('ok', function () {});"]}}]}
    ]},
    {"name": "Orders", "auth": {"type": "apikey", "apikey": [{"key": "key", "value": "X-Key"}, {"key": "value", "value": "k-1"}, {"key": "in", "value": "header"}]},
     "item": [
      {"name": "List", "request": {"method": "GET", "url": {"raw": "{{baseUrl}}/orders?page=1", "query": [{"key": "page", "value": "1"}]}}},
      {"name": "Deeper", "item": [
        {"name": "Read", "request": {"method": "GET", "url": {"raw": "{{baseUrl}}/orders/:id", "variable": [{"key": "id", "value": "42"}]}}}
      ]}
    ]},
    {"name": "Ping", "request": "https://shop.example.com/ping"},
    {"name": "Structured", "request": {"method": "GET", "url": {"protocol": "https", "host": ["shop", "example", "com"], "path": ["a", "b"], "query": [{"key": "x", "value": "1"}, {"key": "off", "value": "2", "disabled": true}]}}}
  ]
}`

func read(t *testing.T, collection, environment string) domain.ImportedCollection {
	t.Helper()

	imported, err := NewReader().Read([]byte(collection), []byte(environment))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	return imported
}

func refusal(t *testing.T, collection, environment string) error {
	t.Helper()

	_, err := NewReader().Read([]byte(collection), []byte(environment))
	if err == nil {
		t.Fatalf("Read accepted a file it should have refused")
	}
	if !errors.Is(err, domain.ErrInvalidCollection) {
		t.Fatalf("error = %v, wanted it to wrap ErrInvalidCollection", err)
	}
	return err
}

func TestRequestsComeOutInTheOrderOfTheCollectionWithTheirFolders(t *testing.T) {
	imported := read(t, shopCollection, "")

	wanted := []string{"Login", "List", "Read", "Ping", "Structured"}
	if len(imported.Requests) != len(wanted) {
		t.Fatalf("%d requests, wanted %d", len(imported.Requests), len(wanted))
	}
	for position, name := range wanted {
		request := imported.Requests[position]
		if request.Name != name || request.Position != position {
			t.Errorf("request %d = %q at %d, wanted %q", position, request.Name, request.Position, name)
		}
	}

	if got := strings.Join(imported.Requests[2].Folders, "/"); got != "Orders/Deeper" {
		t.Errorf("the folders of Read are %q, wanted Orders/Deeper", got)
	}
	if imported.Name != "Shop" {
		t.Errorf("name = %q", imported.Name)
	}
}

func TestARequestIsReadWithItsHeadersBodyAndAddress(t *testing.T) {
	login := read(t, shopCollection, "").Requests[0]

	if login.Method != "POST" || login.URL != "{{baseUrl}}/login" {
		t.Errorf("method = %q, url = %q", login.Method, login.URL)
	}
	if len(login.Headers) != 1 || login.Headers[0].Name != "Accept" {
		t.Errorf("headers = %+v, wanted the enabled one only", login.Headers)
	}
	if login.Body.Mode != "raw" || login.Body.Language != "json" || login.Body.Raw != `{"email":"a@b.c"}` {
		t.Errorf("body = %+v", login.Body)
	}
}

func TestAuthenticationIsInheritedAndNoauthSwitchesItOff(t *testing.T) {
	requests := read(t, shopCollection, "").Requests

	if requests[0].Auth != nil {
		t.Errorf("Login says noauth and still has %+v", requests[0].Auth)
	}
	if requests[1].Auth == nil || requests[1].Auth.Type != "apikey" || requests[1].Auth.Params["key"] != "X-Key" {
		t.Errorf("List should take its folder's key, got %+v", requests[1].Auth)
	}
	if requests[2].Auth == nil || requests[2].Auth.Type != "apikey" {
		t.Errorf("Read is two folders deep and should still inherit, got %+v", requests[2].Auth)
	}
	if requests[3].Auth == nil || requests[3].Auth.Type != "bearer" || requests[3].Auth.Params["token"] != "{{authToken}}" {
		t.Errorf("Ping should take the collection's bearer, got %+v", requests[3].Auth)
	}
}

func TestAnAddressIsReadAsATextAPieceByPieceOrAsABareString(t *testing.T) {
	requests := read(t, shopCollection, "").Requests

	if requests[2].PathVariables["id"] != "42" || requests[2].URL != "{{baseUrl}}/orders/:id" {
		t.Errorf("Read = %+v", requests[2])
	}
	if requests[3].Method != "GET" || requests[3].URL != "https://shop.example.com/ping" {
		t.Errorf("a bare address should be a GET: %+v", requests[3])
	}
	if requests[4].URL != "https://shop.example.com/a/b?x=1" {
		t.Errorf("a structured address was put together as %q", requests[4].URL)
	}
}

func TestScriptsAreRecordedByKindAndSize(t *testing.T) {
	imported := read(t, shopCollection, "")

	if got := imported.Requests[0].Scripts; len(got) != 1 || got[0].Kind != "test" || got[0].Lines != 1 {
		t.Errorf("Login scripts = %+v", got)
	}
	if len(imported.Findings) != 1 || !strings.Contains(imported.Findings[0].Message, "collection") ||
		!strings.Contains(imported.Findings[0].Message, "2 lines") {
		t.Errorf("findings = %+v, wanted the collection's own script", imported.Findings)
	}
}

func TestAnEnvironmentOverridesTheCollectionAndSaysWhichVariablesAreSecret(t *testing.T) {
	environment := `{"name": "staging", "values": [
	  {"key": "baseUrl", "value": "https://staging.example.com", "enabled": true},
	  {"key": "authToken", "value": "from-env", "type": "secret", "enabled": true},
	  {"key": "extra", "value": "1", "enabled": true},
	  {"key": "off", "value": "2", "enabled": false}
	]}`

	byName := map[string]domain.CollectionVariable{}
	for _, variable := range read(t, shopCollection, environment).Variables {
		byName[variable.Name] = variable
	}

	if byName["baseUrl"].Value != "https://staging.example.com" {
		t.Errorf("baseUrl = %+v", byName["baseUrl"])
	}
	if !byName["authToken"].Secret || byName["authToken"].Value != "from-env" {
		t.Errorf("authToken = %+v", byName["authToken"])
	}
	if byName["extra"].Value != "1" {
		t.Errorf("a variable only the environment has was lost: %+v", byName["extra"])
	}
	if _, present := byName["off"]; present {
		t.Error("a disabled variable was kept")
	}
}

func TestOnlyV20AndV21CollectionsAreRead(t *testing.T) {
	refusals := map[string]string{
		"not json":       `nope`,
		"a list":         `[1, 2]`,
		"no schema":      `{"info": {"name": "x"}, "item": []}`,
		"version 1":      `{"id": "x", "name": "old", "requests": []}`,
		"another format": `{"info": {"schema": "https://example.com/other.json"}, "item": []}`,
		"no items":       `{"info": {"schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"}}`,
	}

	for name, text := range refusals {
		t.Run(name, func(t *testing.T) {
			_ = refusal(t, text, "")
		})
	}

	read(t, `{"info": {"schema": "https://schema.getpostman.com/json/collection/v2.0.0/collection.json"}, "item": []}`, "")
}

func TestAFileLargerThanTheLimitIsRefused(t *testing.T) {
	err := refusal(t, strings.Repeat(" ", domain.MaxCollectionBytes+1), "")
	if !strings.Contains(err.Error(), "5 MB") {
		t.Errorf("error = %v, wanted it to name the limit", err)
	}

	_ = refusal(t, shopCollection, strings.Repeat(" ", domain.MaxCollectionBytes+1))
}

func TestAnEnvironmentThatIsNotJSONIsRefused(t *testing.T) {
	err := refusal(t, shopCollection, `{not json`)
	if !strings.Contains(err.Error(), "environment") {
		t.Errorf("error = %v, wanted it to name the environment file", err)
	}
}

func TestFoldersNestedTooDeepAreRefused(t *testing.T) {
	text := `{"request": {"method": "GET", "url": "/a"}, "name": "R"}`
	for depth := 0; depth <= domain.MaxCollectionDepth+1; depth++ {
		text = fmt.Sprintf(`{"name": "f%d", "item": [%s]}`, depth, text)
	}

	err := refusal(t, `{"info": {"schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"}, "item": [`+text+`]}`, "")
	if !strings.Contains(err.Error(), "nested") {
		t.Errorf("error = %v, wanted it to name the nesting", err)
	}
}

func TestMoreRequestsThanTheLimitAreRefused(t *testing.T) {
	var items strings.Builder
	for i := 0; i <= domain.MaxCollectionRequests; i++ {
		if i > 0 {
			items.WriteString(",")
		}
		items.WriteString(`{"name":"r","request":{"method":"GET","url":"/a"}}`)
	}

	err := refusal(t, `{"info": {"schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"}, "item": [`+items.String()+`]}`, "")
	if !strings.Contains(err.Error(), "more than") {
		t.Errorf("error = %v, wanted it to name the limit", err)
	}
}

func TestAHundredRequestsAreReadQuickly(t *testing.T) {
	var folders strings.Builder
	for folder := 0; folder < 10; folder++ {
		if folder > 0 {
			folders.WriteString(",")
		}
		var requests strings.Builder
		for request := 0; request < 10; request++ {
			if request > 0 {
				requests.WriteString(",")
			}
			fmt.Fprintf(&requests, `{"name":"r%d","request":{"method":"POST","url":{"raw":"{{baseUrl}}/f%d/r%d?page=1"},
				"header":[{"key":"Accept","value":"application/json"}],
				"body":{"mode":"raw","raw":"{\"n\":%d}","options":{"raw":{"language":"json"}}}}}`, request, folder, request, request)
		}
		fmt.Fprintf(&folders, `{"name":"folder%d","item":[%s]}`, folder, requests.String())
	}
	text := `{"info": {"schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"}, "item": [` + folders.String() + `]}`

	started := time.Now()
	imported := read(t, text, "")
	elapsed := time.Since(started)

	if len(imported.Requests) != 100 {
		t.Fatalf("%d requests, wanted 100", len(imported.Requests))
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("reading took %v", elapsed)
	}
}

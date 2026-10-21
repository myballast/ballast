package openapi

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/youssefmahersi/ballast/internal/domain"
)

func read(t *testing.T, text string) domain.ImportedSpec {
	t.Helper()

	spec, err := NewReader().Read([]byte(text))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	return spec
}

func refusal(t *testing.T, text string) error {
	t.Helper()

	_, err := NewReader().Read([]byte(text))
	if err == nil {
		t.Fatalf("Read accepted a specification it should have refused")
	}
	if !errors.Is(err, domain.ErrInvalidSpec) {
		t.Fatalf("error = %v, wanted it to wrap ErrInvalidSpec", err)
	}
	return err
}

func endpointOf(t *testing.T, spec domain.ImportedSpec, method, path string) domain.ImportedEndpoint {
	t.Helper()

	endpoint, found := spec.EndpointByRef(domain.EndpointRef{Method: method, Path: path})
	if !found {
		t.Fatalf("the specification has no %s %s; it has %v", method, path, refsOf(spec))
	}
	return endpoint
}

func refsOf(spec domain.ImportedSpec) []string {
	var refs []string
	for _, endpoint := range spec.Endpoints {
		refs = append(refs, endpoint.Ref().String())
	}
	return refs
}

const jsonSpec = `{
	"openapi": "3.0.3",
	"info": {"title": "Shop", "version": "1"},
	"servers": [{"url": "https://api.shop.example.com/v1"}],
	"paths": {
		"/users/{id}": {
			"get": {"tags": ["users"], "summary": "Read one user"},
			"delete": {"tags": ["users"], "operationId": "deleteUser"}
		},
		"/orders": {
			"post": {
				"tags": ["orders"],
				"requestBody": {"content": {"application/json": {"example": {"item": "thing", "count": 2}}}}
			}
		},
		"/health": {"get": {}}
	}
}`

func TestAJSONSpecificationListsItsEndpoints(t *testing.T) {
	spec := read(t, jsonSpec)

	if spec.Title != "Shop" {
		t.Errorf("title = %q", spec.Title)
	}

	user := endpointOf(t, spec, "GET", "/users/{id}")
	if user.Tag != "users" || user.Summary != "Read one user" {
		t.Errorf("GET /users/{id} = %+v", user)
	}
	if len(user.PathParams) != 1 || user.PathParams[0] != "id" {
		t.Errorf("path params = %v, wanted [id]", user.PathParams)
	}

	if endpointOf(t, spec, "DELETE", "/users/{id}").Summary != "deleteUser" {
		t.Errorf("a missing summary should fall back to the operation id")
	}
	if endpointOf(t, spec, "GET", "/health").Tag != "" {
		t.Errorf("an operation with no tag should have an empty tag")
	}
}

func TestEndpointsComeOutInAStableOrderTaggedFirst(t *testing.T) {
	spec := read(t, jsonSpec)

	wanted := []string{"POST /orders", "GET /users/{id}", "DELETE /users/{id}", "GET /health"}
	got := refsOf(spec)
	if strings.Join(got, "|") != strings.Join(wanted, "|") {
		t.Errorf("order = %v, wanted %v", got, wanted)
	}

	for attempt := 0; attempt < 20; attempt++ {
		again := refsOf(read(t, jsonSpec))
		if strings.Join(again, "|") != strings.Join(wanted, "|") {
			t.Fatalf("attempt %d gave %v", attempt, again)
		}
	}
}

func TestAnExampleRequestBodyBecomesCompactJSON(t *testing.T) {
	order := endpointOf(t, read(t, jsonSpec), "POST", "/orders")

	if order.Body != `{"count":2,"item":"thing"}` {
		t.Errorf("body = %q", order.Body)
	}
	if order.ContentType != "application/json" {
		t.Errorf("content type = %q", order.ContentType)
	}
}

func TestTheBasePathOfTheServerIsReportedNotAddedToThePaths(t *testing.T) {
	spec := read(t, jsonSpec)

	if spec.BasePath != "/v1" {
		t.Errorf("base path = %q, wanted /v1", spec.BasePath)
	}
	if len(spec.Notes) != 1 || !strings.Contains(spec.Notes[0], "/v1") {
		t.Errorf("notes = %v, wanted one mentioning /v1", spec.Notes)
	}
	endpointOf(t, spec, "GET", "/health")
}

const yamlSpec = `
openapi: 3.1.0
info:
  title: Pets
  version: "1"
paths:
  /pets:
    parameters:
      - name: X-Tenant
        in: header
        required: true
        example: acme
    get:
      tags: [pets]
      parameters:
        - name: X-Trace
          in: header
          schema:
            default: true
        - name: X-Required-Without-Example
          in: header
          required: true
        - name: X-Optional-Without-Example
          in: header
    post:
      tags: [pets]
      requestBody:
        content:
          application/json:
            examples:
              second: {value: {name: second}}
              first: {value: {name: first}}
  /pets/{pet-id}:
    get:
      tags: [pets]
`

func TestAYAMLSpecificationInVersion31IsRead(t *testing.T) {
	spec := read(t, yamlSpec)

	list := endpointOf(t, spec, "GET", "/pets")
	if list.Headers["X-Tenant"] != "acme" {
		t.Errorf("a path level header was not merged in: %v", list.Headers)
	}
	if list.Headers["X-Trace"] != "true" {
		t.Errorf("X-Trace = %q, wanted the default of the schema", list.Headers["X-Trace"])
	}

	value, present := list.Headers["X-Required-Without-Example"]
	if !present || value != "" {
		t.Errorf("a required header with no example should be there and empty: %v", list.Headers)
	}
	if _, present := list.Headers["X-Optional-Without-Example"]; present {
		t.Errorf("an optional header with no example should not be there: %v", list.Headers)
	}

	if endpointOf(t, spec, "POST", "/pets").Body != `{"name":"first"}` {
		t.Errorf("the first example by name should be used")
	}
	if got := endpointOf(t, spec, "GET", "/pets/{pet-id}").PathParams; len(got) != 1 || got[0] != "pet-id" {
		t.Errorf("path params = %v, wanted the name as written", got)
	}
}

const swagger2Spec = `{
	"swagger": "2.0",
	"info": {"title": "Old API", "version": "1"},
	"basePath": "/api",
	"consumes": ["application/json"],
	"paths": {
		"/items": {
			"put": {
				"tags": ["items"],
				"parameters": [
					{"name": "X-Version", "in": "header", "x-example": "7"},
					{"name": "body", "in": "body", "schema": {"example": {"name": "thing"}}}
				]
			}
		}
	}
}`

func TestASwagger2SpecificationIsRead(t *testing.T) {
	spec := read(t, swagger2Spec)

	put := endpointOf(t, spec, "PUT", "/items")
	if put.Body != `{"name":"thing"}` || put.ContentType != "application/json" {
		t.Errorf("body = %q, content type = %q", put.Body, put.ContentType)
	}
	if put.Headers["X-Version"] != "7" {
		t.Errorf("headers = %v", put.Headers)
	}
	if spec.BasePath != "/api" {
		t.Errorf("base path = %q", spec.BasePath)
	}
}

func TestSwagger2WrittenWithAnUnquotedVersionIsRead(t *testing.T) {
	read(t, "swagger: 2.0\ninfo: {title: T, version: '1'}\npaths: {}\n")
}

func TestARequestBodyWithNoExampleLeavesASkeleton(t *testing.T) {
	spec := read(t, `{"openapi":"3.0.0","paths":{"/a":{"post":{
		"requestBody":{"content":{"application/json":{"schema":{"type":"object"}}}}}}}}`)

	post := endpointOf(t, spec, "POST", "/a")
	if post.Body != "" || post.ContentType != "application/json" {
		t.Errorf("body = %q, content type = %q, wanted an empty body to fill in", post.Body, post.ContentType)
	}
}

func TestAnExampleKeptAsTextIsUsedAsItIs(t *testing.T) {
	spec := read(t, `{"openapi":"3.0.0","paths":{"/a":{"post":{
		"requestBody":{"content":{"application/json":{"example":"{\"name\":\"thing\"}"}}}}}}}`)

	if body := endpointOf(t, spec, "POST", "/a").Body; body != `{"name":"thing"}` {
		t.Errorf("body = %q", body)
	}
}

func TestReferencesToComponentsAreFollowed(t *testing.T) {
	spec := read(t, `{
		"openapi": "3.0.0",
		"paths": {
			"/a": {"$ref": "#/components/pathItems/Shared"},
			"/b": {"post": {
				"parameters": [{"$ref": "#/components/parameters/Trace"}],
				"requestBody": {"$ref": "#/components/requestBodies/Order"}
			}}
		},
		"components": {
			"pathItems": {"Shared": {"get": {"tags": ["shared"]}}},
			"parameters": {"Trace": {"name": "X-Trace", "in": "header", "example": "abc"}},
			"requestBodies": {"Order": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/Order"}}}}},
			"schemas": {"Order": {"type": "object", "example": {"id": 1}}}
		}
	}`)

	if endpointOf(t, spec, "GET", "/a").Tag != "shared" {
		t.Errorf("the path item was not followed")
	}

	post := endpointOf(t, spec, "POST", "/b")
	if post.Headers["X-Trace"] != "abc" {
		t.Errorf("headers = %v", post.Headers)
	}
	if post.Body != `{"id":1}` {
		t.Errorf("body = %q, wanted the example found through the schema reference", post.Body)
	}
}

func TestSchemasThatRefertoThemselvesAreHarmless(t *testing.T) {
	spec := read(t, `{
		"openapi": "3.0.0",
		"paths": {"/tree": {"post": {"requestBody": {"content": {"application/json": {
			"schema": {"$ref": "#/components/schemas/Node"}}}}}}},
		"components": {"schemas": {"Node": {
			"type": "object",
			"properties": {"children": {"type": "array", "items": {"$ref": "#/components/schemas/Node"}}}}}}
	}`)

	if endpointOf(t, spec, "POST", "/tree").Body != "" {
		t.Errorf("a schema with no example should leave the body empty")
	}
}

func TestCredentialsInTheSpecificationNeverReachTheEndpoints(t *testing.T) {
	spec := read(t, `{
		"openapi": "3.0.0",
		"paths": {"/a": {"get": {"parameters": [
			{"name": "Authorization", "in": "header", "example": "Bearer s3cr3t-token"},
			{"name": "Cookie", "in": "header", "example": "session=s3cr3t-cookie"},
			{"name": "X-API-Key", "in": "header", "example": "s3cr3t-key"},
			{"name": "X-Request-Id", "in": "header", "example": "visible"}
		]}}},
		"components": {"securitySchemes": {
			"key": {"type": "apiKey", "in": "header", "name": "X-API-Key"},
			"bearer": {"type": "http", "scheme": "bearer"}
		}}
	}`)

	headers := endpointOf(t, spec, "GET", "/a").Headers
	if len(headers) != 1 || headers["X-Request-Id"] != "visible" {
		t.Errorf("headers = %v, wanted only X-Request-Id", headers)
	}
}

func TestAuthenticationSchemesAreReportedWithoutAnyValue(t *testing.T) {
	spec := read(t, `{
		"openapi": "3.0.0",
		"paths": {},
		"components": {"securitySchemes": {
			"key": {"type": "apiKey", "in": "header", "name": "X-API-Key", "description": "use s3cr3t"},
			"bearer": {"type": "http", "scheme": "bearer"},
			"oauth": {"type": "oauth2", "flows": {"clientCredentials": {"tokenUrl": "https://auth.example.com/token", "scopes": {}}}}
		}}
	}`)

	got := map[string]domain.AuthScheme{}
	for _, scheme := range spec.Schemes {
		got[scheme.Name] = scheme
	}

	if got["key"].Type != "apiKey" || got["key"].Detail != "header X-API-Key" {
		t.Errorf("key = %+v", got["key"])
	}
	if got["bearer"].Detail != "bearer" {
		t.Errorf("bearer = %+v", got["bearer"])
	}
	if got["oauth"].Type != "oauth2" || got["oauth"].Detail != "clientCredentials" {
		t.Errorf("oauth = %+v", got["oauth"])
	}
	if len(spec.Schemes) != 3 || spec.Schemes[0].Name != "bearer" {
		t.Errorf("schemes = %+v, wanted three, sorted by name", spec.Schemes)
	}
}

func TestOperationsThatAScenarioCannotMakeAreSkippedWithANote(t *testing.T) {
	spec := read(t, `{"openapi":"3.0.0","paths":{"/a":{"get":{},"options":{},"trace":{}}}}`)

	if len(spec.Endpoints) != 1 {
		t.Errorf("endpoints = %v, wanted only GET /a", refsOf(spec))
	}
	if len(spec.Notes) != 1 || !strings.Contains(spec.Notes[0], "2") {
		t.Errorf("notes = %v, wanted one counting the two skipped", spec.Notes)
	}
}

func TestAnExampleTooLongToKeepLeavesTheBodyEmptyAndSaysSo(t *testing.T) {
	long := strings.Repeat("a", domain.MaxImportedTextLength)
	spec := read(t, fmt.Sprintf(`{"openapi":"3.0.0","paths":{"/a":{"post":{
		"requestBody":{"content":{"application/json":{"example":{"text":%q}}}}}}}}`, long))

	post := endpointOf(t, spec, "POST", "/a")
	if post.Body != "" {
		t.Errorf("an example longer than a node holds should not be kept")
	}
	if !post.BodyTooLong {
		t.Errorf("the endpoint should say its example was too long")
	}
}

// The refusals. Each one is a file somebody could upload to make Ballast hang,
// read something it should not, or store something it cannot show.

func TestAFileLargerThanTheLimitIsRefused(t *testing.T) {
	err := refusal(t, strings.Repeat(" ", domain.MaxSpecBytes+1))

	if !strings.Contains(err.Error(), "5 MB") {
		t.Errorf("error = %v, wanted it to name the limit", err)
	}
}

func TestSomethingThatIsNotASpecificationIsRefused(t *testing.T) {
	cases := map[string]string{
		"empty":           "",
		"plain text":      "hello",
		"a list":          "- a\n- b\n",
		"broken json":     `{"openapi": "3.0.0",`,
		"no version":      `{"paths": {}}`,
		"another version": `{"openapi": "4.0.0", "paths": {}}`,
		"paths is a list": `{"openapi": "3.0.0", "paths": []}`,
		"no paths":        `{"openapi": "3.0.0"}`,
		"swagger one":     `{"swagger": "1.2", "paths": {}}`,
	}

	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			_ = refusal(t, text)
		})
	}
}

func TestAReferenceOutsideTheFileIsRefusedAndNeverFollowed(t *testing.T) {
	cases := map[string]string{
		"http":     "http://169.254.169.254/latest/meta-data",
		"https":    "https://example.com/spec.yaml#/components/parameters/Id",
		"file":     "file:///etc/passwd",
		"relative": "other.yaml#/components/parameters/Id",
		"parent":   "../shared.yaml",
		"no slash": "#components",
	}

	for name, target := range cases {
		t.Run(name, func(t *testing.T) {
			text := fmt.Sprintf(`{"openapi":"3.0.0","paths":{"/a":{"get":{"parameters":[{"$ref":%q}]}}}}`, target)

			err := refusal(t, text)
			if !strings.Contains(err.Error(), "outside the file") {
				t.Errorf("error = %v, wanted it to say the reference points outside the file", err)
			}
		})
	}
}

func TestAReferenceThatPointsAtNothingIsRefused(t *testing.T) {
	err := refusal(t, `{"openapi":"3.0.0","paths":{"/a":{"get":{"parameters":[{"$ref":"#/components/parameters/Missing"}]}}}}`)

	if !strings.Contains(err.Error(), "points at nothing") {
		t.Errorf("error = %v", err)
	}
}

func TestReferencesThatPointAtEachOtherAreRefused(t *testing.T) {
	err := refusal(t, `{
		"openapi": "3.0.0",
		"paths": {"/a": {"get": {"parameters": [{"$ref": "#/components/parameters/A"}]}}},
		"components": {"parameters": {
			"A": {"$ref": "#/components/parameters/B"},
			"B": {"$ref": "#/components/parameters/A"}
		}}
	}`)

	if !strings.Contains(err.Error(), "circular") {
		t.Errorf("error = %v, wanted it to say circular", err)
	}
}

func TestAReferenceToItselfIsRefused(t *testing.T) {
	err := refusal(t, `{
		"openapi": "3.0.0",
		"paths": {"/a": {"get": {"parameters": [{"$ref": "#/components/parameters/A"}]}}},
		"components": {"parameters": {"A": {"$ref": "#/components/parameters/A"}}}
	}`)

	if !strings.Contains(err.Error(), "circular") {
		t.Errorf("error = %v", err)
	}
}

func TestAChainOfReferencesLongerThanTheLimitIsRefused(t *testing.T) {
	var parameters strings.Builder
	for i := 0; i <= domain.MaxSpecRefDepth+2; i++ {
		fmt.Fprintf(&parameters, `"P%d": {"$ref": "#/components/parameters/P%d"},`, i, i+1)
	}
	end := domain.MaxSpecRefDepth + 3

	text := fmt.Sprintf(`{
		"openapi": "3.0.0",
		"paths": {"/a": {"get": {"parameters": [{"$ref": "#/components/parameters/P0"}]}}},
		"components": {"parameters": {%s "P%d": {"name": "X", "in": "header"}}}
	}`, parameters.String(), end)

	err := refusal(t, text)
	if !strings.Contains(err.Error(), "chain") {
		t.Errorf("error = %v, wanted it to name the chain", err)
	}
}

func TestMoreReferencesThanTheLimitAreRefused(t *testing.T) {
	var parameters strings.Builder
	for i := 0; i <= domain.MaxSpecRefs; i++ {
		parameters.WriteString(`{"$ref": "#/components/parameters/Id"},`)
	}

	text := fmt.Sprintf(`{
		"openapi": "3.0.0",
		"paths": {"/a": {"get": {"parameters": [%s {"name": "last", "in": "query"}]}}},
		"components": {"parameters": {"Id": {"name": "id", "in": "query"}}}
	}`, parameters.String())

	err := refusal(t, text)
	if !strings.Contains(err.Error(), "$ref") {
		t.Errorf("error = %v, wanted it to name the limit on references", err)
	}
}

func TestAnAliasBombInYAMLIsRefusedQuickly(t *testing.T) {
	var bomb strings.Builder
	bomb.WriteString("openapi: 3.0.0\npaths: {}\nx-a: &a [lol, lol, lol, lol, lol, lol, lol, lol, lol]\n")
	previous := "a"
	for level := 'b'; level <= 'k'; level++ {
		name := string(level)
		bomb.WriteString(fmt.Sprintf("x-%s: &%s [", name, name))
		for i := 0; i < 9; i++ {
			bomb.WriteString(fmt.Sprintf("*%s,", previous))
		}
		bomb.WriteString("]\n")
		previous = name
	}

	started := time.Now()
	_, err := NewReader().Read([]byte(bomb.String()))
	elapsed := time.Since(started)

	if elapsed > 2*time.Second {
		t.Fatalf("the alias bomb took %v", elapsed)
	}
	if err == nil || !errors.Is(err, domain.ErrInvalidSpec) {
		t.Fatalf("error = %v, wanted the bomb refused with ErrInvalidSpec", err)
	}
	if !strings.Contains(err.Error(), "aliasing") {
		t.Fatalf("error = %v, wanted it refused for its aliases and not for something else", err)
	}
}

func TestAHundredEndpointsAreReadWellUnderTheTimeTheTicketAllows(t *testing.T) {
	var paths strings.Builder
	for i := 0; i < 100; i++ {
		if i > 0 {
			paths.WriteString(",")
		}
		fmt.Fprintf(&paths, `"/resource%d/{id}": {
			"parameters": [{"$ref": "#/components/parameters/Trace"}],
			"get": {"tags": ["group%d"], "summary": "read %d"},
			"post": {"tags": ["group%d"], "requestBody": {"content": {"application/json": {"example": {"name": "n%d"}}}}}
		}`, i, i%7, i, i%7, i)
	}
	text := fmt.Sprintf(`{"openapi":"3.0.3","info":{"title":"Big"},"paths":{%s},
		"components":{"parameters":{"Trace":{"name":"X-Trace","in":"header","example":"abc"}}}}`, paths.String())

	started := time.Now()
	spec := read(t, text)
	elapsed := time.Since(started)

	if len(spec.Endpoints) != 200 {
		t.Fatalf("%d endpoints, wanted 200", len(spec.Endpoints))
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("reading took %v", elapsed)
	}
}

func TestAHugeExampleIsMeasuredWithoutBeingWrittenOut(t *testing.T) {
	huge := make([]any, 1_000_000)
	for position := range huge {
		huge[position] = "element"
	}

	budget := domain.MaxImportedTextLength
	if fitsInBudget(huge, &budget) {
		t.Fatalf("a list of a million elements fits in a budget of %d", domain.MaxImportedTextLength)
	}
	if budget < -len("element")*2 {
		t.Errorf("the budget went down to %d: the walk went on long after it was spent", budget)
	}
}

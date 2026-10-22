package main

import (
	"strings"
	"testing"

	"github.com/youssefmahersi/ballast/internal/adapter/k6"
	"github.com/youssefmahersi/ballast/internal/adapter/postman"
	"github.com/youssefmahersi/ballast/internal/domain"
)

const pipelineToken = "ghp_a_token_in_a_collection_0123456789abcdef"

const pipelineCollection = `{
  "info": {"name": "Shop", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
  "variable": [{"key": "baseUrl", "value": "https://shop.example.com"}],
  "item": [
    {"name": "Auth", "item": [
      {"name": "Login", "request": {"method": "POST",
        "header": [{"key": "Authorization", "value": "Bearer ` + pipelineToken + `"}],
        "url": {"raw": "{{baseUrl}}/login?remember=1"},
        "body": {"mode": "raw", "raw": "{\"email\":\"a@b.c\",\"password\":\"` + pipelineToken + `\"}", "options": {"raw": {"language": "json"}}}}},
      {"name": "Read", "request": {"method": "GET",
        "url": {"raw": "{{baseUrl}}/users/:id?expand=orders", "variable": [{"key": "id", "value": "42"}]}}}
    ]}
  ]
}`

// The whole path of an import, from the text of a collection to a script: read,
// analysed, written as a graph, generated, and checked by the inspector that every
// scenario passes before it is stored. It lives here because cmd is the one place
// allowed to know every layer.
func TestACollectionBecomesAScriptTheInspectorAcceptsAndNoSecretIsInIt(t *testing.T) {
	collection, err := postman.NewReader().Read([]byte(pipelineCollection), nil)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	analysis, err := domain.AnalyseCollection(collection, []int{0, 1})
	if err != nil {
		t.Fatalf("AnalyseCollection: %v", err)
	}
	graph, err := analysis.Graph()
	if err != nil {
		t.Fatalf("Graph: %v", err)
	}

	script, err := k6.GenerateScript(graph)
	if err != nil {
		t.Fatalf("GenerateScript: %v", err)
	}

	if report := k6.NewScriptInspector().Inspect(script); report.Err() != nil {
		t.Fatalf("the inspector refuses the generated script: %v\n%s", report.Errors(), script)
	}

	if strings.Contains(script, pipelineToken) {
		t.Errorf("the token is in the script:\n%s", script)
	}
	for _, wanted := range []string{
		`__ENV["AUTHORIZATION"]`, `// Auth / Login`, `// Auth / Read`,
		`encodeURIComponent("remember")`, `tags: { name: "POST /login" }`, `tags: { name: "GET /users/{id}" }`,
	} {
		if !strings.Contains(script, wanted) {
			t.Errorf("the script lacks %q:\n%s", wanted, script)
		}
	}

	// The requests run in the order of the collection.
	if strings.Index(script, "// Auth / Login") > strings.Index(script, "// Auth / Read") {
		t.Error("the requests are not in the order of the collection")
	}
}

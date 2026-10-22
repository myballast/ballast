package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func aRequest(position int, name, method, url string) CollectionRequest {
	return CollectionRequest{Position: position, Name: name, Method: method, URL: url}
}

func allOf(collection ImportedCollection) []int {
	positions := make([]int, 0, len(collection.Requests))
	for _, request := range collection.Requests {
		positions = append(positions, request.Position)
	}
	return positions
}

func analyse(t *testing.T, collection ImportedCollection, chosen ...int) CollectionAnalysis {
	t.Helper()

	if len(chosen) == 0 {
		chosen = allOf(collection)
	}
	analysis, err := AnalyseCollection(collection, chosen)
	if err != nil {
		t.Fatalf("AnalyseCollection: %v", err)
	}
	return analysis
}

func graphOf(t *testing.T, analysis CollectionAnalysis) ScenarioGraph {
	t.Helper()

	graph, err := analysis.Graph()
	if err != nil {
		t.Fatalf("Graph: %v", err)
	}
	return graph
}

func requestNodes(graph ScenarioGraph) []Node {
	var nodes []Node
	for _, node := range graph.Nodes {
		if node.Kind == NodeHTTPRequest {
			nodes = append(nodes, node)
		}
	}
	return nodes
}

func findingsAbout(analysis CollectionAnalysis, request string) []string {
	var messages []string
	for _, finding := range analysis.Findings {
		if finding.Request == request {
			messages = append(messages, finding.Message)
		}
	}
	return messages
}

// The secrets of this collection. Each literal is a distinctive string, so that
// finding one anywhere in the output is unambiguous.
var leaked = []string{
	"tok-AAAA1111", "hunter2-BBBB", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.sig_CCCC",
	"literal-DDDD", "literal-EEEE", "session=FFFF", "GGGG-token-literal",
	"hunter2-HHHH", "IIII-key-value", "JJJJ-key", "KKKK-secret", "LLLL-header-key",
	"MMMM-escaped",
}

// A collection with a secret at every place one can be put. The criterion of the
// ticket is that none of it reaches the graph or the logs.
func collectionWithSecretsEverywhere() ImportedCollection {
	login := aRequest(0, "Login", "POST", "{{baseUrl}}/login")
	login.Headers = []CollectionPair{
		{"Authorization", "Bearer literal-DDDD"},
		{"X-Api-Key", "literal-EEEE"},
		{"Cookie", "session=FFFF"},
		{"Accept", "application/json"},
	}
	login.Body = CollectionBody{
		Mode: "raw", Language: "json",
		Raw: `{"email":"a@b.c","password":"hunter2-HHHH","nested":{"api_key":"IIII-key-value"},"keep":"visible"}`,
	}

	bearerLiteral := aRequest(1, "Literal bearer", "GET", "{{baseUrl}}/me")
	bearerLiteral.Auth = &CollectionAuth{Type: "bearer", Params: map[string]string{"token": "GGGG-token-literal"}}

	bearerVariable := aRequest(2, "Variable bearer", "GET", "{{baseUrl}}/orders")
	bearerVariable.Auth = &CollectionAuth{Type: "bearer", Params: map[string]string{"token": "{{authToken}}"}}

	inQuery := aRequest(3, "Key in query", "GET", "{{baseUrl}}/x?api_key=JJJJ-key&page=2")

	form := aRequest(4, "Form", "POST", "{{baseUrl}}/oauth")
	form.Body = CollectionBody{Mode: "urlencoded", Pairs: []CollectionPair{
		{"client_secret", "KKKK-secret"}, {"grant_type", "password"},
	}}

	apiKey := aRequest(5, "API key header", "GET", "{{baseUrl}}/keyed")
	apiKey.Auth = &CollectionAuth{Type: "apikey", Params: map[string]string{
		"key": "X-Token", "value": "LLLL-header-key", "in": "header",
	}}

	escaped := aRequest(6, "Escaped key", "POST", "{{baseUrl}}/escaped")
	escaped.Body = CollectionBody{Mode: "raw", Language: "json", Raw: "{\"pass\\u0077ord\":\"MMMM-escaped\"}"}

	usesVariables := aRequest(7, "Uses variables", "GET", "{{baseUrl}}/vars")
	usesVariables.Headers = []CollectionPair{
		{"X-Env", "{{custom}}"}, {"X-Jwt", "{{harmless}}"},
	}

	return ImportedCollection{
		Name: "Shop",
		Requests: []CollectionRequest{
			login, bearerLiteral, bearerVariable, inQuery, form, apiKey, escaped, usesVariables,
		},
		Variables: []CollectionVariable{
			{Name: "baseUrl", Value: "https://shop.example.com"},
			{Name: "authToken", Value: "tok-AAAA1111"},
			{Name: "custom", Value: "hunter2-BBBB", Secret: true},
			{Name: "harmless", Value: "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.sig_CCCC"},
		},
	}
}

func everything(t *testing.T, analysis CollectionAnalysis) string {
	t.Helper()

	graph, err := analysis.Graph()
	if err != nil {
		t.Fatalf("Graph: %v", err)
	}
	graphJSON, err := json.Marshal(graph)
	if err != nil {
		t.Fatalf("encode the graph: %v", err)
	}
	endpointsJSON, err := json.Marshal(analysis.Endpoints())
	if err != nil {
		t.Fatalf("encode the endpoints: %v", err)
	}
	reportJSON, err := json.Marshal(analysis.Findings)
	if err != nil {
		t.Fatalf("encode the report: %v", err)
	}
	secretsJSON, err := json.Marshal(analysis.Secrets)
	if err != nil {
		t.Fatalf("encode the secrets: %v", err)
	}
	return string(graphJSON) + string(endpointsJSON) + string(reportJSON) + string(secretsJSON)
}

// The acceptance criterion. Nothing the collection holds as a secret may be in
// the graph, in the endpoints a catalog would keep, in the report, or in the list
// of secrets to create — that list is keys only.
func TestNoSecretOfTheCollectionReachesAnyOutput(t *testing.T) {
	analysis := analyse(t, collectionWithSecretsEverywhere())
	output := everything(t, analysis)

	for _, secret := range leaked {
		if strings.Contains(output, secret) {
			t.Errorf("the output holds %q", secret)
		}
	}
}

func TestEverySecretBecomesAKeyToCreateAndAReferenceInTheGraph(t *testing.T) {
	analysis := analyse(t, collectionWithSecretsEverywhere())
	graph := graphOf(t, analysis)

	keys := map[string]bool{}
	for _, secret := range analysis.Secrets {
		keys[secret.Key] = true
		if err := ValidateSecretKey(secret.Key); err != nil {
			t.Errorf("the key %q is not a usable secret key: %v", secret.Key, err)
		}
	}
	for _, wanted := range []string{"AUTH_TOKEN", "CUSTOM", "HARMLESS", "AUTHORIZATION", "X_API_KEY", "COOKIE", "PASSWORD", "API_KEY", "CLIENT_SECRET", "X_TOKEN"} {
		if !keys[wanted] {
			t.Errorf("no secret %s was detected; found %v", wanted, keys)
		}
	}

	// Each variable a secret becomes is set from the environment, never from a value.
	fromEnv := map[string]string{}
	for _, node := range graph.Nodes {
		if node.Kind == NodeSetVariable && node.FromEnv != "" {
			fromEnv[node.Variable] = node.FromEnv
			if node.Value != "" {
				t.Errorf("the variable %s has both a value and a secret", node.Variable)
			}
		}
	}
	if fromEnv["authToken"] != "AUTH_TOKEN" || fromEnv["custom"] != "CUSTOM" {
		t.Errorf("secret variables = %v", fromEnv)
	}
}

func TestTheRequestsKeepTheirShapeOnceTheirSecretsAreOut(t *testing.T) {
	graph := graphOf(t, analyse(t, collectionWithSecretsEverywhere()))
	nodes := requestNodes(graph)

	login := nodes[0]
	if login.Headers["Authorization"] != "Bearer {{authorization}}" && !strings.HasPrefix(login.Headers["Authorization"], "Bearer {{") {
		t.Errorf("Authorization = %q, wanted the scheme kept and the token a variable", login.Headers["Authorization"])
	}
	if login.Headers["Accept"] != "application/json" {
		t.Errorf("an ordinary header was changed: %v", login.Headers)
	}
	if !strings.Contains(login.Body, `"email":"a@b.c"`) || !strings.Contains(login.Body, `"keep":"visible"`) {
		t.Errorf("the rest of the body was lost: %s", login.Body)
	}
	if !strings.Contains(login.Body, `"password":"{{password}}"`) {
		t.Errorf("the password was not made a variable: %s", login.Body)
	}

	if got := nodes[1].Headers["Authorization"]; !strings.HasPrefix(got, "Bearer {{") || strings.Contains(got, "GGGG") {
		t.Errorf("a literal bearer became %q", got)
	}
	if got := nodes[2].Headers["Authorization"]; got != "Bearer {{authToken}}" {
		t.Errorf("a bearer already written as a variable became %q", got)
	}
	if nodes[3].Query[0].Name != "api_key" || !strings.HasPrefix(nodes[3].Query[0].Value, "{{") {
		t.Errorf("the key in the query became %+v", nodes[3].Query)
	}
	if nodes[3].Query[1] != (QueryParam{Name: "page", Value: "2"}) {
		t.Errorf("an ordinary query parameter was changed: %+v", nodes[3].Query)
	}
}

// A JSON key written with an escape cannot be found by text, so the body is
// dropped and said so: dropping is safe, and guessing is not.
func TestABodyWhoseSecretCannotBeRemovedIsDroppedAndReported(t *testing.T) {
	analysis := analyse(t, collectionWithSecretsEverywhere())
	nodes := requestNodes(graphOf(t, analysis))

	if nodes[6].Body != "" {
		t.Errorf("the body was kept: %q", nodes[6].Body)
	}
	if messages := findingsAbout(analysis, "Escaped key"); len(messages) == 0 {
		t.Error("nothing was reported about the body that was dropped")
	}
}

// The last guard. Whatever the conversion did, a value the file marked as secret
// must not be in what comes out; if it is, the import fails rather than writing it.
func TestTheFinalGuardRefusesToWriteASecretItStillHolds(t *testing.T) {
	analysis := analyse(t, collectionWithSecretsEverywhere())

	// A path is not somewhere any rule looks for a secret, so one hidden there
	// is only caught by the guard.
	analysis.Requests[7].Node.Path = "/leaks/tok-AAAA1111"

	if _, err := analysis.Graph(); !errors.Is(err, ErrInvalidCollection) {
		t.Fatalf("Graph error = %v, wanted ErrInvalidCollection", err)
	}
	if strings.Contains(fmt.Sprint(analysis.checkNoSecretIsHeld()), "tok-AAAA1111") {
		t.Error("the refusal repeats the secret")
	}
}

func TestWordsInANameDecideWhetherItLooksLikeASecret(t *testing.T) {
	secret := []string{"token", "authToken", "access_token", "API_KEY", "apiKey", "x-api-key", "password", "clientSecret", "bearer", "JWT", "Authorization"}
	plain := []string{"baseUrl", "author", "authors", "userId", "page", "keyword", "monkey", "tokenizer", "limit"}

	for _, name := range secret {
		if !looksLikeASecretName(name) {
			t.Errorf("%q should look like a secret", name)
		}
	}
	for _, name := range plain {
		if looksLikeASecretName(name) {
			t.Errorf("%q should not look like a secret", name)
		}
	}
}

func TestTheShapeOfAValueDecidesWhetherItLooksLikeASecret(t *testing.T) {
	secret := []string{
		"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.sig", "ghp_abcdefghijklmnopqrstuvwxyz0123456789",
		"sk-abcdefghijklmnopqrstuvwx", "AKIAABCDEFGHIJKLMNOP", "0123456789abcdef0123456789abcdef",
	}
	plain := []string{"", "application/json", "hello world", "https://example.com/a", "12345", "{{token}}", "Bearer {{token}}"}

	for _, value := range secret {
		if !looksLikeASecretValue(value) {
			t.Errorf("%q should look like a secret", value)
		}
	}
	for _, value := range plain {
		if looksLikeASecretValue(value) {
			t.Errorf("%q should not look like a secret", value)
		}
	}
}

func TestAKeyThatIsRefusedAsASecretKeyIsRenamedAndSaid(t *testing.T) {
	request := aRequest(0, "R", "GET", "/x")
	request.Headers = []CollectionPair{{"X-One", "{{K6_TOKEN}}"}, {"X-Two", "{{BASE_URL}}"}}
	collection := ImportedCollection{
		Requests: []CollectionRequest{request},
		Variables: []CollectionVariable{
			{Name: "K6_TOKEN", Value: "a-long-secret-value-1", Secret: true},
			{Name: "BASE_URL", Value: "another-secret-value-2", Secret: true},
		},
	}

	analysis := analyse(t, collection)
	for _, secret := range analysis.Secrets {
		if err := ValidateSecretKey(secret.Key); err != nil {
			t.Errorf("the key %q is refused: %v", secret.Key, err)
		}
	}
	if len(analysis.Secrets) != 2 {
		t.Fatalf("secrets = %+v", analysis.Secrets)
	}
	if len(findingsAbout(analysis, "")) == 0 {
		t.Error("the renaming was not reported")
	}
	if _, err := analysis.Graph(); err != nil {
		t.Errorf("Graph: %v", err)
	}
}

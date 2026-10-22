package domain

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestRequestsInSubfoldersKeepTheOrderOfTheCollection(t *testing.T) {
	first := aRequest(0, "Login", "POST", "/login")
	first.Folders = []string{"Auth"}
	second := aRequest(1, "List", "GET", "/orders")
	second.Folders = []string{"Orders"}
	third := aRequest(2, "Read", "GET", "/orders/1")
	third.Folders = []string{"Orders", "Details"}
	fourth := aRequest(3, "Logout", "POST", "/logout")
	fourth.Folders = []string{"Auth"}

	graph := graphOf(t, analyse(t, ImportedCollection{Requests: []CollectionRequest{first, second, third, fourth}}))
	nodes := requestNodes(graph)

	wanted := []string{"Login", "List", "Read", "Logout"}
	for position, name := range wanted {
		if nodes[position].Label != name {
			t.Errorf("request %d is %q, wanted %q", position, nodes[position].Label, name)
		}
	}
	if nodes[2].Group != "Orders / Details" {
		t.Errorf("the folder of a nested request is %q", nodes[2].Group)
	}
}

func TestTheChosenOrderIsTheOrderOfTheScenario(t *testing.T) {
	collection := ImportedCollection{Requests: []CollectionRequest{
		aRequest(0, "A", "GET", "/a"), aRequest(1, "B", "GET", "/b"), aRequest(2, "C", "GET", "/c"),
	}}

	nodes := requestNodes(graphOf(t, analyse(t, collection, 2, 0, 2)))
	if len(nodes) != 2 || nodes[0].Label != "C" || nodes[1].Label != "A" {
		t.Errorf("nodes = %+v, wanted C then A, each once", nodes)
	}
}

func TestAChoiceOfNothingOrOfAMissingRequestIsRefused(t *testing.T) {
	collection := ImportedCollection{Requests: []CollectionRequest{aRequest(0, "A", "GET", "/a")}}

	for name, chosen := range map[string][]int{"nothing": nil, "a missing one": {0, 9}} {
		if _, err := AnalyseCollection(collection, chosen); !errors.Is(err, ErrInvalidCollection) {
			t.Errorf("%s: error = %v, wanted ErrInvalidCollection", name, err)
		}
	}
}

func TestAnAddressBecomesAPathAMethodAndAQuery(t *testing.T) {
	request := aRequest(0, "Orders", "get", "{{baseUrl}}/users/:id/orders/{{orderId}}?expand=items&q={{term}}&note=a%20b#top")
	request.PathVariables = map[string]string{"id": "42"}
	collection := ImportedCollection{
		Requests: []CollectionRequest{request},
		Variables: []CollectionVariable{
			{Name: "baseUrl", Value: "https://shop.example.com"},
			{Name: "orderId", Value: "7"}, {Name: "term", Value: "shoes"},
		},
	}

	graph := graphOf(t, analyse(t, collection))
	node := requestNodes(graph)[0]

	if node.Method != "GET" || node.Path != "/users/{id}/orders/{orderId}" {
		t.Errorf("method = %q, path = %q", node.Method, node.Path)
	}
	wantedQuery := []QueryParam{{"expand", "items"}, {"q", "{{term}}"}, {"note", "a b"}}
	if fmt.Sprint(node.Query) != fmt.Sprint(wantedQuery) {
		t.Errorf("query = %+v, wanted %+v", node.Query, wantedQuery)
	}

	// The base URL is what BASE_URL replaces: no node sets it.
	values := map[string]string{}
	for _, n := range graph.Nodes {
		if n.Kind == NodeSetVariable {
			values[n.Variable] = n.Value
		}
	}
	if _, set := values["baseUrl"]; set {
		t.Error("the base URL variable was written into the scenario")
	}
	if values["orderId"] != "7" || values["term"] != "shoes" || values["id"] != "42" {
		t.Errorf("variables = %v", values)
	}
}

func TestOnlyTheVariablesTheRequestsUseAreWritten(t *testing.T) {
	collection := ImportedCollection{
		Requests: []CollectionRequest{aRequest(0, "A", "GET", "/a?x={{used}}")},
		Variables: []CollectionVariable{
			{Name: "used", Value: "1"}, {Name: "unused", Value: "2"},
		},
	}

	graph := graphOf(t, analyse(t, collection))
	for _, node := range graph.Nodes {
		if node.Kind == NodeSetVariable && node.Variable == "unused" {
			t.Error("a variable no request uses was written")
		}
	}
}

func TestAVariableUsedByAnotherVariableIsWrittenToo(t *testing.T) {
	collection := ImportedCollection{
		Requests: []CollectionRequest{aRequest(0, "A", "GET", "/a?x={{outer}}")},
		Variables: []CollectionVariable{
			{Name: "inner", Value: "deep"}, {Name: "outer", Value: "{{inner}}-value"},
		},
	}

	var names []string
	for _, node := range graphOf(t, analyse(t, collection)).Nodes {
		if node.Kind == NodeSetVariable {
			names = append(names, node.Variable)
		}
	}
	if fmt.Sprint(names) != "[inner outer]" {
		t.Errorf("variables = %v", names)
	}
}

func TestAPathVariableIsSetOnlyWhenItsValueChanges(t *testing.T) {
	one := aRequest(0, "One", "GET", "/items/:id")
	one.PathVariables = map[string]string{"id": "1"}
	same := aRequest(1, "Same", "GET", "/items/:id/details")
	same.PathVariables = map[string]string{"id": "1"}
	other := aRequest(2, "Other", "GET", "/items/:id")
	other.PathVariables = map[string]string{"id": "2"}

	graph := graphOf(t, analyse(t, ImportedCollection{Requests: []CollectionRequest{one, same, other}}))

	var sets []string
	for _, node := range graph.Nodes {
		if node.Kind == NodeSetVariable {
			sets = append(sets, node.Value)
		}
	}
	if fmt.Sprint(sets) != "[1 2]" {
		t.Errorf("the values set were %v, wanted [1 2]", sets)
	}
}

// Everything a scenario cannot carry is said, with the name of the request: the
// ticket asks for a report, not for silence.
func TestEverythingLeftBehindIsReportedWithTheRequestsName(t *testing.T) {
	scripted := aRequest(0, "Scripted", "GET", "/a")
	scripted.Scripts = []CollectionScript{{Kind: "prerequest", Lines: 4}, {Kind: "test", Lines: 9}}

	basic := aRequest(1, "Basic", "GET", "/b")
	basic.Auth = &CollectionAuth{Type: "basic", Params: map[string]string{"username": "u", "password": "hunter2-NNNN"}}

	form := aRequest(2, "Upload", "POST", "/c")
	form.Body = CollectionBody{Mode: "formdata"}

	dynamic := aRequest(3, "Dynamic", "POST", "/d")
	dynamic.Body = CollectionBody{Mode: "raw", Language: "json", Raw: `{"id":"{{$guid}}","name":"x"}`}

	options := aRequest(4, "Preflight", "OPTIONS", "/e")

	elsewhere := aRequest(5, "Elsewhere", "GET", "https://other.example.com/f")

	collection := ImportedCollection{
		Requests: []CollectionRequest{aRequest(9, "First", "GET", "https://shop.example.com/first"), scripted, basic, form, dynamic, options, elsewhere},
	}
	analysis := analyse(t, collection)

	cases := map[string]string{
		"Scripted":  "pre-request script",
		"Basic":     "basic authentication",
		"Upload":    "formdata",
		"Dynamic":   "{{$guid}}",
		"Preflight": "OPTIONS",
		"Elsewhere": "another host",
	}
	for request, wanted := range cases {
		found := false
		for _, message := range findingsAbout(analysis, request) {
			if strings.Contains(message, wanted) {
				found = true
			}
		}
		if !found {
			t.Errorf("nothing about %q mentions %q; findings: %v", request, wanted, findingsAbout(analysis, request))
		}
	}

	if got := findingsAbout(analysis, "Scripted"); len(got) != 2 {
		t.Errorf("both scripts should be reported, got %v", got)
	}

	nodes := requestNodes(graphOf(t, analysis))
	for _, node := range nodes {
		if node.Label == "Preflight" || node.Label == "Elsewhere" {
			t.Errorf("%q should have been left out", node.Label)
		}
		if node.Label == "Dynamic" && !strings.Contains(node.Body, `"id":""`) {
			t.Errorf("the dynamic variable should be empty: %s", node.Body)
		}
	}
	for _, finding := range analysis.Findings {
		if strings.Contains(finding.Message, "hunter2-NNNN") {
			t.Errorf("a finding repeats a secret: %q", finding.Message)
		}
	}
}

func TestAVariableNobodyDefinesIsReportedOnce(t *testing.T) {
	collection := ImportedCollection{Requests: []CollectionRequest{
		aRequest(0, "A", "GET", "/a?x={{ghost}}"), aRequest(1, "B", "GET", "/b?y={{ghost}}"),
	}}

	count := 0
	for _, finding := range analyse(t, collection).Findings {
		if strings.Contains(finding.Message, "ghost") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("the missing variable was reported %d times, wanted 1", count)
	}
}

func TestABodyKeepsItsContentTypeAndAnUrlencodedBodyIsWritten(t *testing.T) {
	json := aRequest(0, "Json", "POST", "/a")
	json.Body = CollectionBody{Mode: "raw", Language: "json", Raw: `{"a":1}`}
	form := aRequest(1, "Form", "POST", "/b")
	form.Body = CollectionBody{Mode: "urlencoded", Pairs: []CollectionPair{{"grant", "x y"}, {"who", "{{user}}"}}}
	explicit := aRequest(2, "Explicit", "POST", "/c")
	explicit.Headers = []CollectionPair{{"content-type", "application/vnd.api+json"}}
	explicit.Body = CollectionBody{Mode: "raw", Language: "json", Raw: `{}`}

	nodes := requestNodes(graphOf(t, analyse(t, ImportedCollection{
		Requests:  []CollectionRequest{json, form, explicit},
		Variables: []CollectionVariable{{Name: "user", Value: "ada"}},
	})))

	if nodes[0].Headers["Content-Type"] != "application/json" {
		t.Errorf("headers = %v", nodes[0].Headers)
	}
	if nodes[1].Body != "grant=x+y&who={{user}}" || nodes[1].Headers["Content-Type"] != "application/x-www-form-urlencoded" {
		t.Errorf("form body = %q, headers = %v", nodes[1].Body, nodes[1].Headers)
	}
	if len(nodes[2].Headers) != 1 || nodes[2].Headers["content-type"] != "application/vnd.api+json" {
		t.Errorf("an explicit content type was overridden: %v", nodes[2].Headers)
	}
}

func TestABodyTooLongForANodeIsDroppedAndReported(t *testing.T) {
	request := aRequest(0, "Big", "POST", "/a")
	request.Body = CollectionBody{Mode: "raw", Language: "text", Raw: strings.Repeat("a", MaxImportedTextLength+1)}

	analysis := analyse(t, ImportedCollection{Requests: []CollectionRequest{request}})
	if requestNodes(graphOf(t, analysis))[0].Body != "" || len(findingsAbout(analysis, "Big")) == 0 {
		t.Error("a body too long for a node should be dropped with a finding")
	}
}

func TestACollectionTooBigForAScenarioIsRefusedWithTheNumbers(t *testing.T) {
	var requests []CollectionRequest
	for i := 0; i < maxNodesPerGraph; i++ {
		requests = append(requests, aRequest(i, fmt.Sprintf("R%d", i), "GET", "/a"))
	}

	analysis := analyse(t, ImportedCollection{Requests: requests})
	_, err := analysis.Graph()
	if !errors.Is(err, ErrInvalidCollection) || !strings.Contains(err.Error(), "200") {
		t.Fatalf("error = %v, wanted it to name the limit", err)
	}
	// A catalog has no such limit.
	if len(analysis.Endpoints()) != 1 {
		t.Errorf("the catalog should keep one endpoint for one method and path, got %d", len(analysis.Endpoints()))
	}
	if len(analysis.Duplicates) != maxNodesPerGraph-1 {
		t.Errorf("%d duplicates reported, wanted %d", len(analysis.Duplicates), maxNodesPerGraph-1)
	}
}

func TestTheCatalogEndpointsCarryNoCredentialAndKeepTheFolderAsTheirTag(t *testing.T) {
	request := aRequest(0, "Login", "POST", "{{baseUrl}}/login")
	request.Folders = []string{"Auth", "Session"}
	request.Headers = []CollectionPair{{"Authorization", "Bearer literal-DDDD"}, {"X-Api-Key", "k"}, {"Accept", "application/json"}}
	request.Body = CollectionBody{Mode: "raw", Language: "json", Raw: `{"a":1}`}

	analysis := analyse(t, ImportedCollection{Requests: []CollectionRequest{request}})
	endpoints := analysis.Endpoints()

	if len(endpoints) != 1 {
		t.Fatalf("%d endpoints", len(endpoints))
	}
	endpoint := endpoints[0]
	if endpoint.Tag != "Auth / Session" || endpoint.Summary != "Login" || endpoint.ContentType != "application/json" {
		t.Errorf("endpoint = %+v", endpoint)
	}
	if len(endpoint.Headers) != 1 || endpoint.Headers["Accept"] != "application/json" {
		t.Errorf("headers = %v, wanted Accept only", endpoint.Headers)
	}
	if err := endpoint.ValidateForCatalog(); err != nil {
		t.Errorf("the catalog would refuse this endpoint: %v", err)
	}
}

func TestADeepOrWideSecretIsStillFoundInAnArrayOfObjects(t *testing.T) {
	request := aRequest(0, "Batch", "POST", "/batch")
	request.Body = CollectionBody{Mode: "raw", Language: "json",
		Raw: `{"items":[{"name":"a","token":"arrayTOKEN-1234"},{"name":"b","token":"arrayTOKEN-5678"}]}`}

	analysis := analyse(t, ImportedCollection{Requests: []CollectionRequest{request}})
	output := everything(t, analysis)

	for _, secret := range []string{"arrayTOKEN-1234", "arrayTOKEN-5678"} {
		if strings.Contains(output, secret) {
			t.Errorf("%s reached the output", secret)
		}
	}
	if len(analysis.Secrets) != 2 {
		t.Errorf("two different tokens should be two secrets, got %+v", analysis.Secrets)
	}
}

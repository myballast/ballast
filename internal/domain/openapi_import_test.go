package domain

import (
	"errors"
	"strings"
	"testing"
)

func someEndpoint(method, path string) ImportedEndpoint {
	return ImportedEndpoint{Method: method, Path: path}
}

func TestChosenEndpointsBecomeRequestsChainedInTheOrderGiven(t *testing.T) {
	graph, notes, err := GraphFromEndpoints([]ImportedEndpoint{
		someEndpoint("POST", "/orders"),
		someEndpoint("GET", "/users/{id}"),
		someEndpoint("DELETE", "/orders/{id}"),
	})
	if err != nil {
		t.Fatalf("GraphFromEndpoints: %v", err)
	}
	if len(notes) != 0 {
		t.Fatalf("notes = %v, wanted none", notes)
	}

	wantedPaths := []string{"/orders", "/users/{id}", "/orders/{id}"}
	if len(graph.Nodes) != len(wantedPaths)+1 {
		t.Fatalf("%d nodes, wanted %d requests and an end", len(graph.Nodes), len(wantedPaths))
	}
	for position, wanted := range wantedPaths {
		if graph.Nodes[position].Path != wanted {
			t.Errorf("node %d has the path %q, wanted %q", position, graph.Nodes[position].Path, wanted)
		}
	}

	last := graph.Nodes[len(graph.Nodes)-1]
	if last.Kind != NodeEnd {
		t.Errorf("the last node is %q, wanted the end", last.Kind)
	}
	if graph.Start != graph.Nodes[0].ID {
		t.Errorf("the graph starts at %q, wanted the first request %q", graph.Start, graph.Nodes[0].ID)
	}
	if err := graph.Validate(); err != nil {
		t.Errorf("the graph does not validate: %v", err)
	}
}

func TestAnEmptyChoiceIsRefused(t *testing.T) {
	_, _, err := GraphFromEndpoints(nil)
	if !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("error = %v, wanted ErrInvalidSpec", err)
	}
}

func TestMoreEndpointsThanOneScenarioHoldsAreRefused(t *testing.T) {
	endpoints := make([]ImportedEndpoint, 0, maxNodesPerGraph)
	for i := 0; i < maxNodesPerGraph; i++ {
		endpoints = append(endpoints, someEndpoint("GET", "/thing"))
	}

	_, _, err := GraphFromEndpoints(endpoints)
	if !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("error = %v, wanted ErrInvalidSpec", err)
	}
}

func TestTheLargestChoiceStillMakesAValidGraph(t *testing.T) {
	endpoints := make([]ImportedEndpoint, 0, maxImportedEndpoints)
	for i := 0; i < maxImportedEndpoints; i++ {
		endpoints = append(endpoints, someEndpoint("GET", "/thing"))
	}

	if _, _, err := GraphFromEndpoints(endpoints); err != nil {
		t.Fatalf("GraphFromEndpoints: %v", err)
	}
}

func TestNamesBetweenBracesBecomeUsableVariableNames(t *testing.T) {
	cases := map[string]string{
		"/users/{id}":                 "/users/{id}",
		"/users/{user-id}":            "/users/{user_id}",
		"/users/{user.id}/pets/{1st}": "/users/{user_id}/pets/{_1st}",
		"/files/{file name}":          "/files/{file_name}",
		"/plain":                      "/plain",
	}

	for written, wanted := range cases {
		graph, _, err := GraphFromEndpoints([]ImportedEndpoint{someEndpoint("GET", written)})
		if err != nil {
			t.Errorf("%q: %v", written, err)
			continue
		}
		if graph.Nodes[0].Path != wanted {
			t.Errorf("%q became %q, wanted %q", written, graph.Nodes[0].Path, wanted)
		}
	}
}

func TestAnExampleBodyAndItsContentTypeAreKept(t *testing.T) {
	endpoint := someEndpoint("POST", "/orders")
	endpoint.ContentType = "application/json"
	endpoint.Body = `{"item":"thing","count":2}`

	graph, notes, err := GraphFromEndpoints([]ImportedEndpoint{endpoint})
	if err != nil {
		t.Fatalf("GraphFromEndpoints: %v", err)
	}
	if len(notes) != 0 {
		t.Errorf("notes = %v, wanted none", notes)
	}

	node := graph.Nodes[0]
	if node.Body != endpoint.Body {
		t.Errorf("body = %q, wanted the example", node.Body)
	}
	if node.Headers["Content-Type"] != "application/json" {
		t.Errorf("Content-Type = %q", node.Headers["Content-Type"])
	}
}

func TestAnExampleTooLongForANodeBecomesAnEmptyBodyWithANote(t *testing.T) {
	endpoint := someEndpoint("POST", "/orders")
	endpoint.Body = `{"text":"` + strings.Repeat("a", maxNodeTextLength) + `"}`

	graph, notes, err := GraphFromEndpoints([]ImportedEndpoint{endpoint})
	if err != nil {
		t.Fatalf("GraphFromEndpoints: %v", err)
	}
	if graph.Nodes[0].Body != "" {
		t.Errorf("body = %d characters, wanted it empty", len(graph.Nodes[0].Body))
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "POST /orders") {
		t.Errorf("notes = %v, wanted one note naming the endpoint", notes)
	}
}

func TestAnEndpointWhoseExampleWasTooLongGetsANote(t *testing.T) {
	endpoint := someEndpoint("POST", "/orders")
	endpoint.BodyTooLong = true

	_, notes, err := GraphFromEndpoints([]ImportedEndpoint{endpoint})
	if err != nil {
		t.Fatalf("GraphFromEndpoints: %v", err)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "POST /orders") {
		t.Errorf("notes = %v, wanted one naming the endpoint", notes)
	}
}

func TestAnExampleThatOpensAPlaceholderItNeverClosesIsDropped(t *testing.T) {
	endpoint := someEndpoint("POST", "/orders")
	endpoint.Body = `{"template":"{{name"}`

	graph, notes, err := GraphFromEndpoints([]ImportedEndpoint{endpoint})
	if err != nil {
		t.Fatalf("GraphFromEndpoints: %v", err)
	}
	if graph.Nodes[0].Body != "" || len(notes) != 1 {
		t.Errorf("body = %q, notes = %v, wanted an empty body and one note", graph.Nodes[0].Body, notes)
	}
}

func TestAHeaderExampleThatIsAnAbsoluteAddressIsEmptied(t *testing.T) {
	endpoint := someEndpoint("GET", "/orders")
	endpoint.Headers = map[string]string{"Referer": "https://shop.example.com/cart", "X-Trace": "abc"}

	graph, notes, err := GraphFromEndpoints([]ImportedEndpoint{endpoint})
	if err != nil {
		t.Fatalf("GraphFromEndpoints: %v", err)
	}

	headers := graph.Nodes[0].Headers
	if headers["Referer"] != "" {
		t.Errorf("Referer = %q, wanted it emptied", headers["Referer"])
	}
	if headers["X-Trace"] != "abc" {
		t.Errorf("X-Trace = %q, wanted it kept", headers["X-Trace"])
	}
	if len(notes) != 1 {
		t.Errorf("notes = %v, wanted one", notes)
	}
}

func TestAnEndpointThatCannotBeANodeIsLeftOutWithANote(t *testing.T) {
	graph, notes, err := GraphFromEndpoints([]ImportedEndpoint{
		someEndpoint("GET", "no-leading-slash"),
		someEndpoint("GET", "/fine"),
	})
	if err != nil {
		t.Fatalf("GraphFromEndpoints: %v", err)
	}

	if len(graph.Nodes) != 2 || graph.Nodes[0].Path != "/fine" {
		t.Errorf("nodes = %+v, wanted /fine and the end", graph.Nodes)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "no-leading-slash") {
		t.Errorf("notes = %v, wanted one naming the endpoint left out", notes)
	}
}

func TestWhenNoEndpointCanBeANodeTheChoiceIsRefused(t *testing.T) {
	_, _, err := GraphFromEndpoints([]ImportedEndpoint{someEndpoint("GET", "no-leading-slash")})
	if !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("error = %v, wanted ErrInvalidSpec", err)
	}
}

func TestCredentialHeadersAreRecognisedByName(t *testing.T) {
	credentials := []string{"Authorization", "authorization", " Cookie ", "Set-Cookie", "Proxy-Authorization"}
	for _, name := range credentials {
		if !IsCredentialHeader(name) {
			t.Errorf("%q should be a credential header", name)
		}
	}

	ordinary := []string{"Accept", "X-Request-Id", "Authorization-Notes", "Content-Type"}
	for _, name := range ordinary {
		if IsCredentialHeader(name) {
			t.Errorf("%q should not be a credential header", name)
		}
	}
}

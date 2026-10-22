package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestAnOrdinaryEndpointIsKeptByTheCatalog(t *testing.T) {
	endpoint := ImportedEndpoint{
		Method: "POST", Path: "/orders", Tag: "orders", Summary: "Place an order",
		Headers: map[string]string{"X-Trace": "abc"}, ContentType: "application/json", Body: `{"a":1}`,
	}

	if err := endpoint.ValidateForCatalog(); err != nil {
		t.Fatalf("ValidateForCatalog: %v", err)
	}
}

func TestTheCatalogRefusesWhatItWouldNotKeep(t *testing.T) {
	cases := map[string]ImportedEndpoint{
		"unknown method":      {Method: "TRACE", Path: "/a"},
		"no leading slash":    {Method: "GET", Path: "a"},
		"path too long":       {Method: "GET", Path: "/" + strings.Repeat("a", maxCatalogPath)},
		"tag too long":        {Method: "GET", Path: "/a", Tag: strings.Repeat("t", maxCatalogTag+1)},
		"summary too long":    {Method: "GET", Path: "/a", Summary: strings.Repeat("s", maxCatalogSummary+1)},
		"body too long":       {Method: "GET", Path: "/a", Body: strings.Repeat("b", MaxImportedTextLength+1)},
		"header with no name": {Method: "GET", Path: "/a", Headers: map[string]string{" ": "x"}},
		"header value long":   {Method: "GET", Path: "/a", Headers: map[string]string{"X": strings.Repeat("v", MaxImportedTextLength+1)}},
	}

	for name, endpoint := range cases {
		t.Run(name, func(t *testing.T) {
			if err := endpoint.ValidateForCatalog(); !errors.Is(err, ErrInvalidCatalog) {
				t.Fatalf("error = %v, wanted ErrInvalidCatalog", err)
			}
		})
	}
}

func TestTheCatalogNeverKeepsACredentialHeader(t *testing.T) {
	for _, name := range []string{"Authorization", "cookie", "Set-Cookie", "Proxy-Authorization"} {
		endpoint := ImportedEndpoint{Method: "GET", Path: "/a", Headers: map[string]string{name: "s3cr3t"}}

		err := endpoint.ValidateForCatalog()
		if !errors.Is(err, ErrInvalidCatalog) {
			t.Fatalf("%s: error = %v, wanted ErrInvalidCatalog", name, err)
		}
		if strings.Contains(err.Error(), "s3cr3t") {
			t.Errorf("%s: the error repeats the value: %v", name, err)
		}
	}
}

func TestTooManyHeadersAreRefused(t *testing.T) {
	headers := map[string]string{}
	for i := 0; i <= maxCatalogHeaders; i++ {
		headers["X-"+strings.Repeat("a", i+1)] = "v"
	}

	if err := (ImportedEndpoint{Method: "GET", Path: "/a", Headers: headers}).ValidateForCatalog(); !errors.Is(err, ErrInvalidCatalog) {
		t.Fatalf("error = %v, wanted ErrInvalidCatalog", err)
	}
}

func TestASourceNeedsAName(t *testing.T) {
	for _, source := range []string{"", "   ", strings.Repeat("s", maxScenarioNameLength+1)} {
		if err := ValidateCatalogSource(source); !errors.Is(err, ErrInvalidCatalog) {
			t.Errorf("%q: error = %v, wanted ErrInvalidCatalog", shorten(source, 10), err)
		}
	}
	if err := ValidateCatalogSource("shop.yaml"); err != nil {
		t.Errorf("shop.yaml: %v", err)
	}
}

func TestChooseKeepsTheOrderGivenAndEachEndpointOnce(t *testing.T) {
	spec := ImportedSpec{Endpoints: []ImportedEndpoint{
		{Method: "GET", Path: "/a"}, {Method: "POST", Path: "/b"}, {Method: "GET", Path: "/c"},
	}}

	chosen, err := spec.Choose([]EndpointRef{
		{Method: "GET", Path: "/c"}, {Method: "GET", Path: "/a"}, {Method: "GET", Path: "/c"},
	})
	if err != nil {
		t.Fatalf("Choose: %v", err)
	}
	if len(chosen) != 2 || chosen[0].Path != "/c" || chosen[1].Path != "/a" {
		t.Errorf("chosen = %+v, wanted /c then /a", chosen)
	}
}

func TestChooseRefusesAnEndpointTheFileDoesNotHave(t *testing.T) {
	_, err := ImportedSpec{}.Choose([]EndpointRef{{Method: "GET", Path: "/nothing"}})
	if !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("error = %v, wanted ErrInvalidSpec", err)
	}
}

package handler

import (
	"net/http/httptest"
	"testing"
)

func TestListLimit(t *testing.T) {
	for query, want := range map[string]int{"": 0, "?limit=1": 1, "?limit=100": 100, "?limit=5000": 5000} {
		got, err := listLimit(httptest.NewRequest("GET", "/projects/x/runs"+query, nil))
		if err != nil || got != want {
			t.Errorf("%q: got %d, %v; want %d", query, got, err, want)
		}
	}
	for _, query := range []string{"?limit=0", "?limit=-1", "?limit=ten", "?limit=1.5"} {
		if _, err := listLimit(httptest.NewRequest("GET", "/projects/x/runs"+query, nil)); err == nil {
			t.Errorf("%q was accepted", query)
		}
	}
}

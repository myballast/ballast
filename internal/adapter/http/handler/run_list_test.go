package handler

import (
	"encoding/base64"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/usecase/run"
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

func TestCursorRoundTrips(t *testing.T) {
	want := run.Cursor{
		// Nanoseconds survive: PostgreSQL stores microseconds, and a cursor
		// that dropped them would skip the runs created within the same second.
		CreatedAt: time.Date(2026, 10, 13, 12, 0, 0, 123456000, time.UTC),
		ID:        uuid.New(),
	}

	got, err := decodeCursor(encodeCursor(want))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) || got.ID != want.ID {
		t.Errorf("round trip gave %+v, want %+v", got, want)
	}
}

func TestCursorRefusesWhatItDidNotWrite(t *testing.T) {
	for _, raw := range []string{
		"not base64!",
		base64.RawURLEncoding.EncodeToString([]byte("no separator")),
		base64.RawURLEncoding.EncodeToString([]byte("yesterday " + uuid.NewString())),
		base64.RawURLEncoding.EncodeToString([]byte("2026-10-13T12:00:00Z not-a-uuid")),
	} {
		if _, err := decodeCursor(raw); err == nil {
			t.Errorf("%q was accepted as a cursor", raw)
		}
	}
}

func TestListQueryReadsTheFiltersAndTheWindow(t *testing.T) {
	scenario, environment := uuid.New(), uuid.New()
	cursor := run.Cursor{CreatedAt: time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC), ID: uuid.New()}

	query, err := listQuery(httptest.NewRequest("GET",
		"/projects/x/runs?status=failed&scenario_id="+scenario.String()+"&environment_id="+environment.String()+
			"&limit=25&after="+encodeCursor(cursor), nil))
	if err != nil {
		t.Fatalf("listQuery: %v", err)
	}

	if query.Filter.Status != "failed" || query.Limit != 25 {
		t.Errorf("status %q limit %d", query.Filter.Status, query.Limit)
	}
	if query.Filter.ScenarioID == nil || *query.Filter.ScenarioID != scenario ||
		query.Filter.EnvironmentID == nil || *query.Filter.EnvironmentID != environment {
		t.Errorf("filter %+v", query.Filter)
	}
	if query.Window.Before != nil || query.Window.After == nil || query.Window.After.ID != cursor.ID {
		t.Errorf("window %+v, want only after", query.Window)
	}
}

func TestListQueryWithNothingAsksForEverything(t *testing.T) {
	query, err := listQuery(httptest.NewRequest("GET", "/projects/x/runs", nil))
	if err != nil {
		t.Fatalf("listQuery: %v", err)
	}
	if query.Filter.Status != "" || query.Filter.ScenarioID != nil || query.Filter.EnvironmentID != nil ||
		query.Window.Before != nil || query.Window.After != nil {
		t.Errorf("an empty query asked for %+v", query)
	}
}

func TestListQueryRefusesWhatIsNotAnIdOrACursor(t *testing.T) {
	for _, q := range []string{
		"?scenario_id=checkout",
		"?environment_id=1234",
		"?before=not-a-cursor",
		"?after=not-a-cursor",
	} {
		if _, err := listQuery(httptest.NewRequest("GET", "/projects/x/runs"+q, nil)); err == nil {
			t.Errorf("%q was accepted", q)
		}
	}
}

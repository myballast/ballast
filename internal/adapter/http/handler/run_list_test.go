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

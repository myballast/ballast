package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/adapter/http/dto"
	"github.com/youssefmahersi/ballast/internal/adapter/http/handler"
	"github.com/youssefmahersi/ballast/internal/adapter/http/session"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/audit"
)

// The journal says who did each thing: a person, a token, or Ballast alone. What
// these tests hold in place is that the response keeps the three apart. An entry
// with no author is Ballast acting alone, so a token's entry must name the token,
// and must not name a person.
//
// Written before the code, which is why they do not compile yet.

type storedJournal struct{ entries []domain.AuditEntry }

func (j storedJournal) InProject(context.Context, uuid.UUID, uuid.UUID, int) ([]domain.AuditEntry, error) {
	return j.entries, nil
}

func readJournal(t *testing.T, entries ...domain.AuditEntry) (dto.AuditListResponse, string) {
	t.Helper()

	h := handler.NewAudit(audit.NewService(storedJournal{entries: entries}, everyone{}, discard()), discard())

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.SetPathValue("projectId", uuid.NewString())
	request = request.WithContext(session.WithIdentity(request.Context(), domain.Identity{UserID: uuid.New(), OrgID: uuid.New()}))

	recorder := httptest.NewRecorder()
	h.InProject(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body)
	}
	var response dto.AuditListResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("the response is not a journal: %v", err)
	}
	return response, recorder.Body.String()
}

func TestTheJournalNamesTheTokenThatActed(t *testing.T) {
	token, person := uuid.New(), uuid.New()
	at := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

	response, body := readJournal(t,
		domain.Allow(uuid.New(), nil, domain.ActionRunQueued, domain.TargetRun, at).By(domain.Actor{TokenID: &token}),
		domain.Allow(uuid.New(), &person, domain.ActionRunQueued, domain.TargetRun, at),
		domain.Allow(uuid.New(), nil, domain.ActionRunQueued, domain.TargetRun, at),
	)

	if len(response.Entries) != 3 {
		t.Fatalf("%d entries, want 3", len(response.Entries))
	}

	byToken, byPerson, byNobody := response.Entries[0], response.Entries[1], response.Entries[2]
	if byToken.ActorTokenID != token.String() || byToken.ActorID != "" {
		t.Errorf("a token's entry names the token %q and the person %q", byToken.ActorTokenID, byToken.ActorID)
	}
	if byPerson.ActorID != person.String() || byPerson.ActorTokenID != "" {
		t.Errorf("a person's entry names the person %q and the token %q", byPerson.ActorID, byPerson.ActorTokenID)
	}
	// The one entry with no author at all is Ballast acting alone.
	if byNobody.ActorID != "" || byNobody.ActorTokenID != "" {
		t.Errorf("Ballast's own entry names %q and %q", byNobody.ActorID, byNobody.ActorTokenID)
	}

	// Absence is spelled by leaving the field out, as it is for the person.
	if strings.Count(body, `"actor_token_id"`) != 1 || strings.Count(body, `"actor_id"`) != 1 {
		t.Errorf("each field should appear once, for the one entry that has it: %s", body)
	}
}

func TestTheJournalNamesTheWebhookThatActedAndNobodyElse(t *testing.T) {
	hook := uuid.New()
	at := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

	response, body := readJournal(t,
		domain.Allow(uuid.New(), nil, domain.ActionRunQueued, domain.TargetRun, at).By(domain.Actor{WebhookID: &hook}),
		domain.Allow(uuid.New(), nil, domain.ActionRunQueued, domain.TargetRun, at),
	)

	byWebhook, byNobody := response.Entries[0], response.Entries[1]
	if byWebhook.ActorWebhookID != hook.String() || byWebhook.ActorID != "" || byWebhook.ActorTokenID != "" {
		t.Errorf("a webhook's entry names %q, the person %q and the token %q", byWebhook.ActorWebhookID, byWebhook.ActorID, byWebhook.ActorTokenID)
	}
	// An entry that names no webhook says nothing of one, and an entry by a webhook is
	// not Ballast acting alone.
	if byNobody.ActorWebhookID != "" {
		t.Errorf("Ballast's own entry names the webhook %q", byNobody.ActorWebhookID)
	}
	if strings.Count(body, `"actor_webhook_id"`) != 1 {
		t.Errorf("the field should appear once, for the one entry that has it: %s", body)
	}
}

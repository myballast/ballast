package handler_test

import (
	"bytes"
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
	"github.com/youssefmahersi/ballast/internal/usecase/apitoken"
)

// The three routes an administrator manages tokens through. What these tests
// hold in place is what a caller can see: the secret in one response and in no
// other, the hash in none, and a status that tells a refused request from a
// missing token from a ceiling that was reached.
//
// Written before the code, which is why they do not compile yet.

type tokenClock struct{ at time.Time }

func (c tokenClock) Now() time.Time { return c.at }

var tokenNow = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

// tokenStore is a registry in memory. It keeps the hash, as the real one does, so
// that a test can look for it in a response.
type tokenStore struct {
	tokens  map[uuid.UUID]domain.APIToken
	hashes  map[uuid.UUID]string
	names   map[uuid.UUID]string
	tooMany bool
}

func newTokenStore() *tokenStore {
	return &tokenStore{
		tokens: map[uuid.UUID]domain.APIToken{},
		hashes: map[uuid.UUID]string{},
		names:  map[uuid.UUID]string{},
	}
}

func (s *tokenStore) ByID(_ context.Context, _, id uuid.UUID) (domain.APIToken, bool, error) {
	token, ok := s.tokens[id]
	return token, ok, nil
}

func (s *tokenStore) Create(_ context.Context, token domain.APIToken, hash string, _ domain.AuditEntry) error {
	if s.tooMany {
		return domain.ErrTooManyAPITokens
	}
	s.tokens[token.ID] = token
	s.hashes[token.ID] = hash
	return nil
}

func (s *tokenStore) List(_ context.Context, _, _ uuid.UUID) ([]apitoken.Listed, error) {
	listed := []apitoken.Listed{}
	for id, token := range s.tokens {
		listed = append(listed, apitoken.Listed{APIToken: token, CreatedByName: s.names[id]})
	}
	return listed, nil
}

func (s *tokenStore) Revoke(_ context.Context, token domain.APIToken, at time.Time, _ domain.AuditEntry) error {
	stored, ok := s.tokens[token.ID]
	if !ok || stored.RevokedAt != nil {
		return domain.ErrNotFound
	}
	stored.RevokedAt = &at
	s.tokens[token.ID] = stored
	return nil
}

func tokenHandler(authz apitoken.Authorizer) (*handler.APIToken, *tokenStore) {
	store := newTokenStore()
	random := bytes.NewReader(bytes.Repeat([]byte{5}, 4096))
	manager := apitoken.NewManager(store, authz, tokenClock{at: tokenNow}, random, discard())
	return handler.NewAPIToken(manager, discard()), store
}

func tokenRequest(method, body string, pathValues map[string]string) *http.Request {
	request := httptest.NewRequest(method, "/", strings.NewReader(body))
	request.SetPathValue("projectId", uuid.NewString())
	for name, value := range pathValues {
		request.SetPathValue(name, value)
	}

	who := domain.Identity{UserID: uuid.New(), OrgID: uuid.New()}
	return request.WithContext(session.WithIdentity(request.Context(), who))
}

func decodeCreated(t *testing.T, recorder *httptest.ResponseRecorder) dto.CreatedAPIToken {
	t.Helper()

	var created dto.CreatedAPIToken
	if err := json.Unmarshal(recorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("the response is not a created token: %v: %s", err, recorder.Body)
	}
	return created
}

// --- Create -------------------------------------------------------------------

func TestCreatingATokenAnswers201WithTheSecretOnce(t *testing.T) {
	h, store := tokenHandler(everyone{})

	recorder := httptest.NewRecorder()
	h.Create(recorder, tokenRequest(http.MethodPost, `{"name":"github main"}`, nil))

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body)
	}
	// The secret is in this body, and a body with a secret in it is not for a
	// cache, a proxy or a browser's history.
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}

	created := decodeCreated(t, recorder)
	if !domain.LooksLikeAPIToken(created.Secret) {
		t.Errorf("the secret %q does not look like a token", created.Secret)
	}
	if created.Token.Name != "github main" || created.Token.State != "active" {
		t.Errorf("token = %+v", created.Token)
	}
	if created.Token.Prefix != domain.APITokenDisplayPrefix(created.Secret) {
		t.Errorf("prefix %q is not the start of the secret", created.Token.Prefix)
	}
	if len(store.tokens) != 1 {
		t.Errorf("%d tokens stored, want 1", len(store.tokens))
	}

	// The hash is how the token is kept, and it is not for anybody to read.
	for id, hash := range store.hashes {
		if strings.Contains(recorder.Body.String(), hash) || strings.Contains(strings.ToLower(recorder.Body.String()), "hash") {
			t.Errorf("the response carries the hash of token %s", id)
		}
	}
}

func TestACreationHonoursARequestedExpiry(t *testing.T) {
	h, _ := tokenHandler(everyone{})
	in30 := tokenNow.Add(30 * 24 * time.Hour)

	body, _ := json.Marshal(dto.CreateAPITokenRequest{Name: "ci", ExpiresAt: &in30})
	recorder := httptest.NewRecorder()
	h.Create(recorder, tokenRequest(http.MethodPost, string(body), nil))

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body)
	}
	if got := decodeCreated(t, recorder).Token.ExpiresAt; !got.Equal(in30) {
		t.Errorf("expires at %v, want %v", got, in30)
	}
}

func TestACreationThatIsWrongSaysWhy(t *testing.T) {
	past := tokenNow.Add(-time.Hour)
	pastBody, _ := json.Marshal(dto.CreateAPITokenRequest{Name: "ci", ExpiresAt: &past})

	cases := map[string]struct {
		body string
		want int
	}{
		"not JSON":                  {`{"name":`, http.StatusBadRequest},
		"no body":                   {``, http.StatusBadRequest},
		"no name":                   {`{"name":"  "}`, http.StatusUnprocessableEntity},
		"a name too long":           {`{"name":"` + strings.Repeat("a", 65) + `"}`, http.StatusUnprocessableEntity},
		"an expiry in the past":     {string(pastBody), http.StatusUnprocessableEntity},
		"an expiry that is no date": {`{"name":"ci","expires_at":"tomorrow"}`, http.StatusBadRequest},
	}

	for name, c := range cases {
		h, store := tokenHandler(everyone{})

		recorder := httptest.NewRecorder()
		h.Create(recorder, tokenRequest(http.MethodPost, c.body, nil))

		if recorder.Code != c.want {
			t.Errorf("%s: status = %d, want %d: %s", name, recorder.Code, c.want, recorder.Body)
		}
		if len(store.tokens) != 0 {
			t.Errorf("%s: a token was written", name)
		}
		if strings.Contains(recorder.Body.String(), "blt_") {
			t.Errorf("%s: the response carries something that looks like a secret", name)
		}
	}
}

// A body that is not a name and a date is somebody's mistake or somebody's
// attack, and it is not read to its end.
func TestABodyThatIsFarTooLargeIsRefused(t *testing.T) {
	h, store := tokenHandler(everyone{})

	recorder := httptest.NewRecorder()
	h.Create(recorder, tokenRequest(http.MethodPost, `{"name":"`+strings.Repeat("a", 1<<20)+`"}`, nil))

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", recorder.Code)
	}
	if len(store.tokens) != 0 {
		t.Error("a token was written for a body that was refused")
	}
}

func TestReachingTheCeilingIs409AndNamesTheLimit(t *testing.T) {
	h, store := tokenHandler(everyone{})
	store.tooMany = true

	recorder := httptest.NewRecorder()
	h.Create(recorder, tokenRequest(http.MethodPost, `{"name":"ci"}`, nil))

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", recorder.Code, recorder.Body)
	}
	if !strings.Contains(recorder.Body.String(), "20") {
		t.Errorf("the message does not name the limit: %s", recorder.Body)
	}
	if strings.Contains(recorder.Body.String(), "blt_") {
		t.Error("a secret went out with a refusal")
	}
}

func TestSomebodyWhoMayNotManageTheProjectIs403OnEveryRoute(t *testing.T) {
	h, store := tokenHandler(nobody{})
	id := uuid.NewString()

	for name, call := range map[string]func(http.ResponseWriter, *http.Request){
		"create": h.Create, "list": h.List, "revoke": h.Revoke,
	} {
		recorder := httptest.NewRecorder()
		call(recorder, tokenRequest(http.MethodPost, `{"name":"ci"}`, map[string]string{"tokenId": id}))

		if recorder.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403: %s", name, recorder.Code, recorder.Body)
		}
	}
	if len(store.tokens) != 0 {
		t.Error("a token was written for somebody who may not")
	}
}

func TestAProjectIdThatIsNotAUUIDIs400(t *testing.T) {
	h, _ := tokenHandler(everyone{})

	for name, call := range map[string]func(http.ResponseWriter, *http.Request){
		"create": h.Create, "list": h.List, "revoke": h.Revoke,
	} {
		recorder := httptest.NewRecorder()
		call(recorder, tokenRequest(http.MethodPost, `{"name":"ci"}`, map[string]string{"projectId": "nope", "tokenId": uuid.NewString()}))

		if recorder.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, recorder.Code)
		}
	}
}

// --- List ---------------------------------------------------------------------

func TestTheListSaysWhatBecameOfEachTokenAndNeverTheSecretOrTheHash(t *testing.T) {
	h, store := tokenHandler(everyone{})

	// One made through the handler, so that its secret is known, then withdrawn.
	recorder := httptest.NewRecorder()
	h.Create(recorder, tokenRequest(http.MethodPost, `{"name":"made here"}`, nil))
	created := decodeCreated(t, recorder)

	revokedAt := tokenNow.Add(-time.Hour)
	author := uuid.New()
	old := domain.APIToken{
		ID: uuid.New(), Name: "withdrawn", Prefix: "blt_Zz9yXx8w", ExpiresAt: tokenNow.Add(time.Hour),
		RevokedAt: &revokedAt, CreatedBy: &author, CreatedAt: tokenNow.Add(-48 * time.Hour),
	}
	store.tokens[old.ID] = old
	store.hashes[old.ID] = "deadbeef-hash"
	store.names[old.ID] = "Grace Hopper"
	used := tokenNow.Add(-time.Minute)
	expired := domain.APIToken{
		ID: uuid.New(), Name: "lapsed", Prefix: "blt_Qq1wEe2r", ExpiresAt: tokenNow.Add(-time.Hour),
		LastUsedAt: &used, CreatedAt: tokenNow.Add(-72 * time.Hour),
	}
	store.tokens[expired.ID] = expired

	listed := httptest.NewRecorder()
	h.List(listed, tokenRequest(http.MethodGet, "", nil))

	if listed.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", listed.Code, listed.Body)
	}
	var response dto.APITokenListResponse
	if err := json.Unmarshal(listed.Body.Bytes(), &response); err != nil {
		t.Fatalf("the response is not a list: %v", err)
	}

	if response.Limit != domain.MaxAPITokensPerProject {
		t.Errorf("limit = %d, want %d: the screen must not carry its own copy of the ceiling", response.Limit, domain.MaxAPITokensPerProject)
	}
	if len(response.Tokens) != 3 {
		t.Fatalf("%d tokens listed, want 3", len(response.Tokens))
	}

	byName := map[string]dto.APIToken{}
	for _, tok := range response.Tokens {
		byName[tok.Name] = tok
	}
	if byName["made here"].State != "active" || byName["lapsed"].State != "expired" || byName["withdrawn"].State != "revoked" {
		t.Errorf("states: %v / %v / %v", byName["made here"].State, byName["lapsed"].State, byName["withdrawn"].State)
	}
	if byName["withdrawn"].RevokedAt == nil || byName["withdrawn"].CreatedByName != "Grace Hopper" || byName["withdrawn"].CreatedBy != author.String() {
		t.Errorf("withdrawn = %+v", byName["withdrawn"])
	}
	if byName["lapsed"].LastUsedAt == nil {
		t.Error("the last use is not reported")
	}
	if byName["lapsed"].RevokedAt != nil || byName["lapsed"].CreatedByName != "" {
		t.Errorf("lapsed = %+v", byName["lapsed"])
	}

	// The secret was shown once, in the creation. Not here.
	if strings.Contains(listed.Body.String(), created.Secret) {
		t.Error("the list carries the secret")
	}
	for _, hash := range store.hashes {
		if strings.Contains(listed.Body.String(), hash) {
			t.Error("the list carries a hash")
		}
	}
	if strings.Contains(strings.ToLower(listed.Body.String()), "hash") || strings.Contains(listed.Body.String(), "secret") {
		t.Errorf("the list has a field it should not have: %s", listed.Body)
	}
}

func TestAnEmptyProjectListsAnEmptyArray(t *testing.T) {
	h, _ := tokenHandler(everyone{})

	recorder := httptest.NewRecorder()
	h.List(recorder, tokenRequest(http.MethodGet, "", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	// An array, not null: the screen maps over it.
	if !strings.Contains(recorder.Body.String(), `"tokens":[]`) {
		t.Errorf("body = %s, want an empty array", recorder.Body)
	}
}

// --- Revoke -------------------------------------------------------------------

func TestRevokingATokenAnswers204(t *testing.T) {
	h, store := tokenHandler(everyone{})
	project := uuid.NewString()

	created := httptest.NewRecorder()
	h.Create(created, tokenRequest(http.MethodPost, `{"name":"ci"}`, map[string]string{"projectId": project}))
	token := decodeCreated(t, created).Token

	recorder := httptest.NewRecorder()
	h.Revoke(recorder, tokenRequest(http.MethodDelete, "", map[string]string{"projectId": project, "tokenId": token.ID}))

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", recorder.Code, recorder.Body)
	}
	if store.tokens[uuid.MustParse(token.ID)].RevokedAt == nil {
		t.Error("the token was not revoked")
	}
}

// A token of another project is not found, whichever way it is not.
func TestATokenOfAnotherProjectIs404(t *testing.T) {
	h, store := tokenHandler(everyone{})

	created := httptest.NewRecorder()
	h.Create(created, tokenRequest(http.MethodPost, `{"name":"ci"}`, nil))
	token := decodeCreated(t, created).Token

	recorder := httptest.NewRecorder()
	h.Revoke(recorder, tokenRequest(http.MethodDelete, "", map[string]string{"tokenId": token.ID}))

	if recorder.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", recorder.Code)
	}
	if store.tokens[uuid.MustParse(token.ID)].RevokedAt != nil {
		t.Error("a token was revoked through another project")
	}
}

func TestAMissingTokenIs404AndAMalformedIdIs400(t *testing.T) {
	h, _ := tokenHandler(everyone{})

	recorder := httptest.NewRecorder()
	h.Revoke(recorder, tokenRequest(http.MethodDelete, "", map[string]string{"tokenId": uuid.NewString()}))
	if recorder.Code != http.StatusNotFound {
		t.Errorf("an unknown token: status = %d, want 404", recorder.Code)
	}

	recorder = httptest.NewRecorder()
	h.Revoke(recorder, tokenRequest(http.MethodDelete, "", map[string]string{"tokenId": "nope"}))
	if recorder.Code != http.StatusBadRequest {
		t.Errorf("a malformed id: status = %d, want 400", recorder.Code)
	}
}

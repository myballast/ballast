//go:build integration

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/adapter/postgres"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/scenario"
)

const (
	collectionToken    = "ghp_a_collection_token_that_must_stay_secret_1"
	collectionPassword = "a-collection-password-that-must-stay-secret-2"
)

// A Postman collection with a token and a password written in clear.
var tokenCollection = fmt.Sprintf(`{
  "info": {"name": "Shop", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
  "variable": [{"key": "baseUrl", "value": "https://not-the-target.example.com"}],
  "item": [
    {"name": "Auth", "item": [
      {"name": "Me", "request": {"method": "GET",
        "header": [{"key": "Authorization", "value": "Bearer %s"}],
        "url": {"raw": "{{baseUrl}}/me?page=2"}}},
      {"name": "Create", "request": {"method": "POST",
        "header": [],
        "url": {"raw": "{{baseUrl}}/items"},
        "body": {"mode": "raw", "raw": "{\"name\":\"thing\",\"password\":\"%s\"}", "options": {"raw": {"language": "json"}}}}}
    ]}
  ]
}`, collectionToken, collectionPassword)

// An imported collection is a scenario that runs, and the credentials of its file
// arrive from the environment's secrets: the target gets them, and nothing stored
// or recorded holds them.
func TestAnImportedCollectionRunsWithItsSecretsFromTheEnvironment(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	type received struct{ method, uri, authorization, body string }
	var mu sync.Mutex
	var requests []received

	target := newTarget(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		requests = append(requests, received{r.Method, r.URL.RequestURI(), r.Header.Get("Authorization"), string(body)})
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))

	db, err := postgres.Open(ctx, dsn())
	if err != nil {
		t.Fatalf("open the database: %v", err)
	}
	t.Cleanup(db.Close)

	org, _, environment := seed(ctx, t, db, target, nil)
	imports := newImporter(ctx, t, db, org)

	imported, _, result, err := imports.service.ImportCollection(ctx, imports.who, scenario.CollectionImport{
		ProjectID:  imports.projectID,
		Format:     domain.FormatPostman,
		Name:       "shop collection",
		Collection: []byte(tokenCollection),
		Requests:   []int{0, 1},
	})
	if err != nil {
		t.Fatalf("ImportCollection: %v", err)
	}

	// The scenario as stored, read back from the database.
	var storedGraph, storedScript string
	if err := db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT graph::text, script FROM scenario WHERE org_id = $1 AND id = $2`, org, imported.ID,
		).Scan(&storedGraph, &storedScript)
	}); err != nil {
		t.Fatalf("read the stored scenario: %v", err)
	}
	for _, secret := range []string{collectionToken, collectionPassword} {
		if strings.Contains(storedGraph, secret) || strings.Contains(storedScript, secret) {
			t.Fatalf("a secret of the file is stored in the scenario: %q", secret)
		}
	}

	// The person creates the secrets the import listed, typing the values.
	values := map[string]string{"authorization": collectionToken, "password": collectionPassword}
	keys := map[string]bool{}
	for _, secret := range result.Secrets {
		value, known := values[secret.Variable]
		if !known {
			t.Fatalf("an unexpected secret was listed: %+v", secret)
		}
		keys[secret.Key] = true
		if _, err := postgres.NewSecretRepository(db, cipher(t)).
			Put(ctx, org, environment, secret.Key, value, time.Now().UTC()); err != nil {
			t.Fatalf("store the secret %s: %v", secret.Key, err)
		}
	}
	if len(keys) != 2 {
		t.Fatalf("secrets listed = %+v, wanted the token and the password", result.Secrets)
	}

	s := build(t, db, org)
	status, queued := s.enqueue(t, fmt.Sprintf(
		`{"scenario_id":%q,"environment_id":%q,"profile":"smoke","params":{"target_vus":1,"plateau_s":2}}`,
		imported.ID, environment))
	if status != http.StatusAccepted {
		t.Fatalf("POST /runs returned %d", status)
	}
	s.processOne(ctx, t)

	finished := s.get(t, queued.ID)
	if finished.Status != string(domain.StatusPassed) {
		t.Fatalf("the run ended as %s: %s", finished.Status, finished.ErrorMessage)
	}

	mu.Lock()
	defer mu.Unlock()

	var me, create int
	for _, request := range requests {
		switch {
		case request.method == "GET" && request.uri == "/me?page=2":
			me++
			if request.authorization != "Bearer "+collectionToken {
				t.Errorf("the target received the Authorization %q", request.authorization)
			}
		case request.method == "POST" && request.uri == "/items":
			create++
			var body map[string]string
			if err := json.Unmarshal([]byte(request.body), &body); err != nil || body["password"] != collectionPassword || body["name"] != "thing" {
				t.Errorf("the target received the body %q", request.body)
			}
		default:
			t.Errorf("an unexpected request: %+v", request)
		}
	}
	if me == 0 || create == 0 {
		t.Errorf("the target received %d of the first request and %d of the second, wanted both", me, create)
	}

	// Nothing the run recorded about itself carries either secret.
	for _, secret := range []string{collectionToken, collectionPassword} {
		if strings.Contains(finished.ErrorMessage, secret) {
			t.Errorf("the run's error message carries %q", secret)
		}
	}
}

// A recording from a browser, with a cookie and a token in it, is a scenario that
// runs: the target receives the token from the environment, and no cookie at all.
func TestAnImportedRecordingRunsWithItsTokenFromTheEnvironmentAndNoCookie(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	const sessionCookie = "sess-a-session-cookie-that-must-never-be-sent-1"
	const token = "ghp_a_recorded_token_that_must_stay_secret_1"

	var mu sync.Mutex
	var cookies, authorizations []string
	target := newTarget(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		cookies = append(cookies, r.Header.Get("Cookie"))
		authorizations = append(authorizations, r.Header.Get("Authorization"))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))

	db, err := postgres.Open(ctx, dsn())
	if err != nil {
		t.Fatalf("open the database: %v", err)
	}
	t.Cleanup(db.Close)

	org, _, environment := seed(ctx, t, db, target, nil)
	imports := newImporter(ctx, t, db, org)

	recording := fmt.Sprintf(`{"log": {"entries": [
	  {"_resourceType": "xhr", "request": {"method": "GET", "url": "https://elsewhere.example.com/api/me?page=2",
	    "headers": [{"name": "Cookie", "value": "sid=%s"}, {"name": "Authorization", "value": "Bearer %s"},
	                {"name": "User-Agent", "value": "Mozilla/5.0"}]},
	   "response": {"content": {"mimeType": "application/json"}}}]}}`, sessionCookie, token)

	imported, _, result, err := imports.service.ImportCollection(ctx, imports.who, scenario.CollectionImport{
		ProjectID:  imports.projectID,
		Format:     domain.FormatHAR,
		Name:       "recorded",
		Collection: []byte(recording),
		Requests:   []int{0},
	})
	if err != nil {
		t.Fatalf("ImportCollection: %v", err)
	}

	var storedGraph, storedScript string
	if err := db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT graph::text, script FROM scenario WHERE org_id = $1 AND id = $2`, org, imported.ID,
		).Scan(&storedGraph, &storedScript)
	}); err != nil {
		t.Fatalf("read the stored scenario: %v", err)
	}
	for _, secret := range []string{sessionCookie, token} {
		if strings.Contains(storedGraph, secret) || strings.Contains(storedScript, secret) {
			t.Fatalf("a secret of the recording is stored in the scenario: %q", secret)
		}
	}

	if len(result.Secrets) != 1 {
		t.Fatalf("secrets listed = %+v, wanted the token only", result.Secrets)
	}
	if _, err := postgres.NewSecretRepository(db, cipher(t)).
		Put(ctx, org, environment, result.Secrets[0].Key, token, time.Now().UTC()); err != nil {
		t.Fatalf("store the secret: %v", err)
	}

	s := build(t, db, org)
	status, queued := s.enqueue(t, fmt.Sprintf(
		`{"scenario_id":%q,"environment_id":%q,"profile":"smoke","params":{"target_vus":1,"plateau_s":2}}`,
		imported.ID, environment))
	if status != http.StatusAccepted {
		t.Fatalf("POST /runs returned %d", status)
	}
	s.processOne(ctx, t)

	finished := s.get(t, queued.ID)
	if finished.Status != string(domain.StatusPassed) {
		t.Fatalf("the run ended as %s: %s", finished.Status, finished.ErrorMessage)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(authorizations) == 0 {
		t.Fatal("the target received nothing")
	}
	for index, header := range authorizations {
		if header != "Bearer "+token {
			t.Errorf("request %d: Authorization = %q", index, header)
		}
		if cookies[index] != "" {
			t.Errorf("request %d: the target received a Cookie, %q", index, cookies[index])
		}
	}
}

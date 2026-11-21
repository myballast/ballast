package apitoken_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/apitoken"
)

// Authenticating a token is the one place a request proves who it is without a
// session. What these tests defend: a dead or unknown token is refused the same
// way whichever it is, a database that is down is never mistaken for a wrong
// credential, and the secret never reaches a log line or an error message.
//
// Written before the code, which is why they do not compile yet.

type fakeClock struct{ at time.Time }

func (c fakeClock) Now() time.Time { return c.at }

type touch struct {
	org, token uuid.UUID
	at         time.Time
}

type fakeRepository struct {
	byHash   map[string]apitoken.Found
	lookups  []string
	touches  []touch
	lookErr  error
	touchErr error
}

func (f *fakeRepository) ByHash(_ context.Context, hash string) (apitoken.Found, bool, error) {
	f.lookups = append(f.lookups, hash)
	if f.lookErr != nil {
		return apitoken.Found{}, false, f.lookErr
	}
	found, ok := f.byHash[hash]
	return found, ok, nil
}

func (f *fakeRepository) Touch(_ context.Context, org, token uuid.UUID, at time.Time) error {
	f.touches = append(f.touches, touch{org, token, at})
	return f.touchErr
}

var now = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

// issued makes a real secret and registers what the database would hold for it.
func issued(t *testing.T, repo *fakeRepository, mutate func(*apitoken.Found)) (secret string, found apitoken.Found) {
	t.Helper()

	secret, err := domain.NewAPITokenSecret(bytes.NewReader(bytes.Repeat([]byte{9}, 64)))
	if err != nil {
		t.Fatalf("NewAPITokenSecret: %v", err)
	}
	found = apitoken.Found{
		TokenID:   uuid.New(),
		OrgID:     uuid.New(),
		ProjectID: uuid.New(),
		ExpiresAt: now.Add(24 * time.Hour),
	}
	if mutate != nil {
		mutate(&found)
	}
	repo.byHash = map[string]apitoken.Found{domain.HashAPITokenSecret(secret): found}
	return secret, found
}

func service(repo *fakeRepository, logs *bytes.Buffer) *apitoken.Service {
	return apitoken.NewService(repo, fakeClock{at: now}, slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
}

func TestAValidTokenAuthenticatesAsATokenWithNoPerson(t *testing.T) {
	repo := &fakeRepository{}
	secret, found := issued(t, repo, nil)
	var logs bytes.Buffer

	who, err := service(repo, &logs).Authenticate(context.Background(), secret)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	if who.OrgID != found.OrgID || who.TokenID != found.TokenID {
		t.Errorf("identity %+v, want org %s and token %s", who, found.OrgID, found.TokenID)
	}
	if who.UserID != uuid.Nil {
		t.Errorf("a token authenticated as the person %s", who.UserID)
	}
	if !who.IsToken() {
		t.Error("the identity does not say it is a token")
	}
}

// The database is asked by hash, never by the secret: the secret is not stored,
// and it must not be sent anywhere it could be logged, such as a query.
func TestTheRepositoryIsAskedByHashNeverBySecret(t *testing.T) {
	repo := &fakeRepository{}
	secret, _ := issued(t, repo, nil)
	var logs bytes.Buffer

	if _, err := service(repo, &logs).Authenticate(context.Background(), secret); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	if len(repo.lookups) != 1 || repo.lookups[0] != domain.HashAPITokenSecret(secret) {
		t.Errorf("lookups %v, want exactly the hash of the secret", repo.lookups)
	}
	for _, l := range repo.lookups {
		if strings.Contains(l, secret) {
			t.Error("the secret itself was sent to the repository")
		}
	}
}

// A value that cannot be a token never costs a query. This is the cheap refusal
// that keeps garbage, and a flood of it, away from the database.
func TestSomethingThatIsNotATokenNeverReachesTheDatabase(t *testing.T) {
	repo := &fakeRepository{}
	var logs bytes.Buffer
	svc := service(repo, &logs)

	for _, value := range []string{"", "blt_", "hello", strings.Repeat("A", 10_000), "Bearer x", "blt_ spaces are not allowed here at all......"} {
		_, err := svc.Authenticate(context.Background(), value)
		if !errors.Is(err, domain.ErrUnauthenticated) {
			t.Errorf("%.40q: error %v, want ErrUnauthenticated", value, err)
		}
	}
	if len(repo.lookups) != 0 {
		t.Errorf("%d values that are not tokens reached the repository", len(repo.lookups))
	}
}

func TestUnknownRevokedAndExpiredTokensAreRefusedTheSameWay(t *testing.T) {
	revokedAt := now.Add(-time.Hour)

	cases := map[string]func(*apitoken.Found){
		"revoked":               func(f *apitoken.Found) { f.RevokedAt = &revokedAt },
		"expired":               func(f *apitoken.Found) { f.ExpiresAt = now.Add(-time.Second) },
		"expiring right now":    func(f *apitoken.Found) { f.ExpiresAt = now },
		"revoked and expired":   func(f *apitoken.Found) { f.RevokedAt = &revokedAt; f.ExpiresAt = now.Add(-time.Hour) },
		"revoked in the future": func(f *apitoken.Found) { at := now.Add(time.Hour); f.RevokedAt = &at },
	}

	var messages []string
	for name, mutate := range cases {
		repo := &fakeRepository{}
		secret, _ := issued(t, repo, mutate)
		var logs bytes.Buffer

		who, err := service(repo, &logs).Authenticate(context.Background(), secret)
		if !errors.Is(err, domain.ErrUnauthenticated) {
			t.Errorf("%s: error %v, want ErrUnauthenticated", name, err)
		}
		if who != (domain.Identity{}) {
			t.Errorf("%s: an identity %+v came back with the refusal", name, who)
		}
		if len(repo.touches) != 0 {
			t.Errorf("%s: a refused token was recorded as used", name)
		}
		messages = append(messages, err.Error())
	}

	// And the same as a token nobody issued.
	repo := &fakeRepository{byHash: map[string]apitoken.Found{}}
	unknown, _ := domain.NewAPITokenSecret(bytes.NewReader(bytes.Repeat([]byte{4}, 64)))
	var logs bytes.Buffer
	_, err := service(repo, &logs).Authenticate(context.Background(), unknown)
	if !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("an unknown token: error %v, want ErrUnauthenticated", err)
	}
	messages = append(messages, err.Error())

	// Telling an expired token from a revoked one from a forged one helps nobody
	// but whoever is forging. The reason goes to the logs.
	for _, m := range messages[1:] {
		if m != messages[0] {
			t.Errorf("refusals differ in what they say: %q and %q", messages[0], m)
		}
	}
}

// A database that is down is not a wrong credential. Answering 401 would send
// every pipeline to rotate a perfectly good token during an outage.
func TestADatabaseFailureIsNotARefusal(t *testing.T) {
	outage := errors.New("connection refused")
	repo := &fakeRepository{lookErr: outage}
	secret, _ := issued(t, repo, nil)
	var logs bytes.Buffer

	_, err := service(repo, &logs).Authenticate(context.Background(), secret)
	if err == nil {
		t.Fatal("a database failure authenticated the token")
	}
	if errors.Is(err, domain.ErrUnauthenticated) {
		t.Error("a database failure was reported as a refusal of the credential")
	}
	if !errors.Is(err, outage) {
		t.Errorf("the cause was lost: %v", err)
	}
}

// Recording that a token was used is information for the person who looks at
// the list. It never decides whether the pipeline runs.
func TestUseIsRecordedWithoutBeingAConditionOfUse(t *testing.T) {
	repo := &fakeRepository{}
	secret, found := issued(t, repo, nil)
	var logs bytes.Buffer

	if _, err := service(repo, &logs).Authenticate(context.Background(), secret); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if len(repo.touches) != 1 {
		t.Fatalf("%d uses recorded, want 1", len(repo.touches))
	}
	got := repo.touches[0]
	if got.org != found.OrgID || got.token != found.TokenID || !got.at.Equal(now) {
		t.Errorf("recorded %+v, want org %s token %s at %v", got, found.OrgID, found.TokenID, now)
	}

	failing := &fakeRepository{touchErr: errors.New("write failed")}
	secret, _ = issued(t, failing, nil)
	logs.Reset()

	if _, err := service(failing, &logs).Authenticate(context.Background(), secret); err != nil {
		t.Errorf("a failed write of last_used_at refused the token: %v", err)
	}
	if !strings.Contains(logs.String(), "write failed") {
		t.Errorf("the failed write was not logged: %q", logs.String())
	}
}

// The secret is a credential. It is not in an error, not in a log line at any
// level, and not in the log of a refusal either, which is where somebody
// debugging a pipeline is most tempted to put it.
func TestTheSecretNeverAppearsInAnErrorOrALog(t *testing.T) {
	revokedAt := now.Add(-time.Hour)

	for name, mutate := range map[string]func(*apitoken.Found){
		"valid":   nil,
		"revoked": func(f *apitoken.Found) { f.RevokedAt = &revokedAt },
		"expired": func(f *apitoken.Found) { f.ExpiresAt = now.Add(-time.Hour) },
	} {
		repo := &fakeRepository{touchErr: errors.New("write failed")}
		secret, _ := issued(t, repo, mutate)
		var logs bytes.Buffer

		_, err := service(repo, &logs).Authenticate(context.Background(), secret)

		if err != nil && strings.Contains(err.Error(), secret) {
			t.Errorf("%s: the secret is in the error: %v", name, err)
		}
		if strings.Contains(logs.String(), secret) {
			t.Errorf("%s: the secret is in the logs: %s", name, logs.String())
		}
		// Nor the shape of it: whoever reads a log must not be able to rebuild a
		// credential from its start.
		if strings.Contains(logs.String(), secret[:len(secret)/2]) {
			t.Errorf("%s: half of the secret is in the logs", name)
		}
	}
}

// What the logs do say about a refusal is which token it was and why, so that
// the person who owns the pipeline can be told without the reason ever reaching
// the caller.
func TestARefusalIsLoggedWithItsReasonAndTheTokenId(t *testing.T) {
	revokedAt := now.Add(-time.Hour)
	repo := &fakeRepository{}
	secret, found := issued(t, repo, func(f *apitoken.Found) { f.RevokedAt = &revokedAt })
	var logs bytes.Buffer

	_, _ = service(repo, &logs).Authenticate(context.Background(), secret)

	out := logs.String()
	if !strings.Contains(out, found.TokenID.String()) {
		t.Errorf("the log does not name the token: %s", out)
	}
	if !strings.Contains(out, "revoked") {
		t.Errorf("the log does not say why: %s", out)
	}
}

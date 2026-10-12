package auth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// Fakes, not mocks: they record what they were given and hand back what the
// test set up.

type fakeAccounts struct {
	me    Me
	meErr error

	existing  []domain.Identity
	lookupErr error
	createErr error

	lookedUp []string
	created  []NewAccount
}

func (f *fakeAccounts) Lookup(_ context.Context, subject string) ([]domain.Identity, error) {
	f.lookedUp = append(f.lookedUp, subject)
	return f.existing, f.lookupErr
}

func (f *fakeAccounts) Me(_ context.Context, who domain.Identity) (Me, error) {
	if f.meErr != nil {
		return Me{}, f.meErr
	}
	if who.UserID != f.me.UserID || who.OrgID != f.me.OrgID {
		return Me{}, domain.ErrNotFound
	}
	return f.me, nil
}

func (f *fakeAccounts) Create(_ context.Context, account NewAccount) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.created = append(f.created, account)
	return nil
}

type fixedClock struct{ at time.Time }

func (c fixedClock) Now() time.Time { return c.at }

type fakeProvider struct {
	identity domain.ExternalIdentity
	err      error

	gotCode, gotVerifier string
}

func (f *fakeProvider) AuthCodeURL(state, verifier string) string {
	return "https://provider.test/authorize?state=" + state + "&verifier=" + verifier
}

func (f *fakeProvider) Exchange(_ context.Context, code, verifier string) (domain.ExternalIdentity, error) {
	f.gotCode, f.gotVerifier = code, verifier
	return f.identity, f.err
}

var now = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

func newService(accounts Accounts, providers map[string]IdentityProvider) *Service {
	return NewService(accounts, providers, fixedClock{at: now}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

var ada = domain.ExternalIdentity{
	Subject: "github|42",
	Email:   "ada@example.test",
	Name:    "Ada Lovelace",
}

func TestFirstSignInCreatesAnOrganizationTheUserAdministers(t *testing.T) {
	accounts := &fakeAccounts{}

	got, err := newService(accounts, nil).SignIn(context.Background(), ada)
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}

	if len(accounts.created) != 1 {
		t.Fatalf("created %d accounts, want 1", len(accounts.created))
	}
	account := accounts.created[0]

	if got.OrgID != account.OrgID || got.UserID != account.UserID {
		t.Errorf("SignIn returned %+v, want the identity it created (%s, %s)", got, account.OrgID, account.UserID)
	}
	for name, id := range map[string]uuid.UUID{"org": account.OrgID, "user": account.UserID, "project": account.ProjectID} {
		if id == uuid.Nil {
			t.Errorf("the %s id was left empty", name)
		}
	}
	if account.Subject != ada.Subject || account.Email != ada.Email || account.Name != ada.Name {
		t.Errorf("the user was created as %q %q %q, want what the provider said", account.Subject, account.Email, account.Name)
	}
	if account.Role != domain.RoleAdmin {
		t.Errorf("the first user's role is %q, want admin", account.Role)
	}
	if account.OrgName == "" || account.ProjectName == "" || account.ProjectSlug == "" {
		t.Errorf("the organization and project need names: %+v", account)
	}
	if !account.CreatedAt.Equal(now) {
		t.Errorf("created at %v, want the clock's %v", account.CreatedAt, now)
	}
}

func TestSignInFindsAnExistingUserWithoutCreatingAnything(t *testing.T) {
	existing := domain.Identity{UserID: uuid.New(), OrgID: uuid.New()}
	accounts := &fakeAccounts{existing: []domain.Identity{existing}}

	got, err := newService(accounts, nil).SignIn(context.Background(), ada)
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}

	if got != existing {
		t.Errorf("SignIn returned %+v, want the existing %+v", got, existing)
	}
	if len(accounts.created) != 0 {
		t.Errorf("an existing user caused %d accounts to be created", len(accounts.created))
	}
	if len(accounts.lookedUp) != 1 || accounts.lookedUp[0] != ada.Subject {
		t.Errorf("looked up %v, want exactly the subject %q", accounts.lookedUp, ada.Subject)
	}
}

// Until invitations exist a person belongs to one organization. If that ever
// stops being true, the oldest membership wins rather than an arbitrary one.
func TestSignInPicksTheOldestOrganization(t *testing.T) {
	oldest := domain.Identity{UserID: uuid.New(), OrgID: uuid.New()}
	accounts := &fakeAccounts{existing: []domain.Identity{oldest, {UserID: uuid.New(), OrgID: uuid.New()}}}

	got, err := newService(accounts, nil).SignIn(context.Background(), ada)
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	if got != oldest {
		t.Errorf("SignIn returned %+v, want the oldest %+v", got, oldest)
	}
}

func TestSignInRefusesAnIncompleteIdentity(t *testing.T) {
	cases := map[string]domain.ExternalIdentity{
		"no subject": {Email: "ada@example.test"},
		"no email":   {Subject: "github|42"},
	}

	for name, identity := range cases {
		t.Run(name, func(t *testing.T) {
			accounts := &fakeAccounts{}

			_, err := newService(accounts, nil).SignIn(context.Background(), identity)
			if !errors.Is(err, domain.ErrIncompleteIdentity) {
				t.Errorf("SignIn returned %v, want ErrIncompleteIdentity", err)
			}
			if len(accounts.lookedUp)+len(accounts.created) != 0 {
				t.Error("an incomplete identity reached the database")
			}
		})
	}
}

func TestSignInReportsStorageFailures(t *testing.T) {
	broken := errors.New("connection reset")

	if _, err := newService(&fakeAccounts{lookupErr: broken}, nil).SignIn(context.Background(), ada); !errors.Is(err, broken) {
		t.Errorf("a failed lookup returned %v", err)
	}
	if _, err := newService(&fakeAccounts{createErr: broken}, nil).SignIn(context.Background(), ada); !errors.Is(err, broken) {
		t.Errorf("a failed creation returned %v", err)
	}
}

func TestCompleteExchangesTheCodeThenSignsIn(t *testing.T) {
	provider := &fakeProvider{identity: ada}
	accounts := &fakeAccounts{}

	got, err := newService(accounts, map[string]IdentityProvider{"github": provider}).
		Complete(context.Background(), "github", "the-code", "the-verifier")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if provider.gotCode != "the-code" || provider.gotVerifier != "the-verifier" {
		t.Errorf("the provider was given %q and %q", provider.gotCode, provider.gotVerifier)
	}
	if len(accounts.created) != 1 || got.UserID != accounts.created[0].UserID {
		t.Errorf("Complete did not sign the identity in: %+v", got)
	}
}

func TestCompleteStopsWhenTheProviderRefuses(t *testing.T) {
	refused := errors.New("bad_verification_code")
	accounts := &fakeAccounts{}

	_, err := newService(accounts, map[string]IdentityProvider{"github": &fakeProvider{err: refused}}).
		Complete(context.Background(), "github", "the-code", "the-verifier")
	if !errors.Is(err, refused) {
		t.Errorf("Complete returned %v, want the provider's error", err)
	}
	if len(accounts.lookedUp) != 0 {
		t.Error("a refused exchange still reached the database")
	}
}

func TestUnknownProviderIsRefused(t *testing.T) {
	service := newService(&fakeAccounts{}, map[string]IdentityProvider{"github": &fakeProvider{}})

	if _, err := service.LoginURL("gitlab", "state", "verifier"); !errors.Is(err, ErrUnknownProvider) {
		t.Errorf("LoginURL returned %v, want ErrUnknownProvider", err)
	}
	if _, err := service.Complete(context.Background(), "gitlab", "code", "verifier"); !errors.Is(err, ErrUnknownProvider) {
		t.Errorf("Complete returned %v, want ErrUnknownProvider", err)
	}
}

func TestProvidersAreListedInAStableOrder(t *testing.T) {
	service := newService(&fakeAccounts{}, map[string]IdentityProvider{
		"google": &fakeProvider{},
		"github": &fakeProvider{},
	})

	got := service.Providers()
	if len(got) != 2 || got[0] != "github" || got[1] != "google" {
		t.Errorf("Providers() = %v, want [github google]", got)
	}
}

func TestMeReadsTheSignedInUserAndOrganization(t *testing.T) {
	me := Me{UserID: uuid.New(), Name: "Ada", Email: "ada@example.test", OrgID: uuid.New(), OrgName: "Ada's organization"}

	got, err := newService(&fakeAccounts{me: me}, nil).Me(context.Background(), domain.Identity{UserID: me.UserID, OrgID: me.OrgID})
	if err != nil {
		t.Fatalf("Me: %v", err)
	}
	if got != me {
		t.Errorf("Me returned %+v, want %+v", got, me)
	}
}

// A session can outlive its user: the account deleted, the token still valid
// for a few hours. That is not a session any more, and it is reported as such.
func TestMeOfAVanishedUserIsUnauthenticated(t *testing.T) {
	_, err := newService(&fakeAccounts{}, nil).Me(context.Background(), domain.Identity{UserID: uuid.New(), OrgID: uuid.New()})
	if !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("Me returned %v, want ErrUnauthenticated", err)
	}
}

package webhook_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/webhook"
)

// Managing a webhook is what a project administrator does from the screen: make it,
// replace its secret, remove it. What these tests hold in place is what holds for API
// tokens, and for the same reasons:
//
//   - the permission is checked here and not only by the route, before anything is read;
//   - a token or a webhook can never manage a webhook, whatever the authorizer says: one
//     that could would outlive its own removal;
//   - the secret exists in the result of Create and Rotate and nowhere else: not in the
//     journal, not in a log, and not in what Get returns;
//   - what is written and what is recorded travel together, or not at all.
//
// Written before the code, which is why they do not compile yet.

type fakeAuthorizer struct {
	allowed bool
	err     error
	asked   []domain.Permission
	project []uuid.UUID
}

func (f *fakeAuthorizer) Can(_ context.Context, _ domain.Identity, perm domain.Permission, project uuid.UUID) (bool, error) {
	f.asked = append(f.asked, perm)
	f.project = append(f.project, project)
	return f.allowed, f.err
}

type fakeRegistry struct {
	hook    *domain.Webhook
	readErr error

	created []domain.Webhook
	secrets []string
	entries []domain.AuditEntry
	rotated []domain.Webhook
	deleted []domain.Webhook
	reads   int

	createErr error
}

func (f *fakeRegistry) ByProject(_ context.Context, _, _ uuid.UUID) (domain.Webhook, bool, error) {
	f.reads++
	if f.readErr != nil {
		return domain.Webhook{}, false, f.readErr
	}
	if f.hook == nil {
		return domain.Webhook{}, false, nil
	}
	return *f.hook, true, nil
}

func (f *fakeRegistry) Create(_ context.Context, hook domain.Webhook, secret string, entry domain.AuditEntry) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.created, f.secrets, f.entries = append(f.created, hook), append(f.secrets, secret), append(f.entries, entry)
	return nil
}

func (f *fakeRegistry) Rotate(_ context.Context, hook domain.Webhook, secret string, _ time.Time, entry domain.AuditEntry) error {
	f.rotated, f.secrets, f.entries = append(f.rotated, hook), append(f.secrets, secret), append(f.entries, entry)
	return nil
}

func (f *fakeRegistry) Delete(_ context.Context, hook domain.Webhook, entry domain.AuditEntry) error {
	f.deleted, f.entries = append(f.deleted, hook), append(f.entries, entry)
	return nil
}

type managerRig struct {
	manager  *webhook.Manager
	registry *fakeRegistry
	authz    *fakeAuthorizer
	logs     *bytes.Buffer
	who      domain.Identity
	project  uuid.UUID
}

func newManagerRig(t *testing.T) *managerRig {
	t.Helper()

	r := &managerRig{
		registry: &fakeRegistry{}, authz: &fakeAuthorizer{allowed: true}, logs: &bytes.Buffer{},
		who: domain.Identity{UserID: uuid.New(), OrgID: uuid.New()}, project: uuid.New(),
	}
	// Not a repeated byte: two secrets drawn from it must differ.
	stream := make([]byte, 256)
	for i := range stream {
		stream[i] = byte(i)
	}
	random := bytes.NewReader(stream)
	r.manager = webhook.NewManager(r.registry, r.authz, clock{at: theMoment}, random,
		slog.New(slog.NewTextHandler(r.logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	return r
}

func existing(r *managerRig) domain.Webhook {
	hook := domain.Webhook{ID: uuid.New(), OrgID: r.who.OrgID, ProjectID: r.project, CreatedAt: theMoment.Add(-time.Hour)}
	r.registry.hook = &hook
	return hook
}

// --- The permission --------------------------------------------------------------------------

func TestTheManagersAreRefusedBeforeAnythingIsReadOrWritten(t *testing.T) {
	for name, call := range map[string]func(*managerRig) error{
		"create": func(r *managerRig) error {
			_, err := r.manager.Create(context.Background(), r.who, r.project)
			return err
		},
		"rotate": func(r *managerRig) error {
			_, err := r.manager.Rotate(context.Background(), r.who, r.project)
			return err
		},
		"delete": func(r *managerRig) error { return r.manager.Delete(context.Background(), r.who, r.project) },
		"get": func(r *managerRig) error {
			_, _, err := r.manager.Get(context.Background(), r.who, r.project)
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := newManagerRig(t)
			existing(r)
			r.authz.allowed = false

			if err := call(r); !errors.Is(err, domain.ErrForbidden) {
				t.Fatalf("got %v, want ErrForbidden", err)
			}
			if r.registry.reads != 0 || len(r.registry.created)+len(r.registry.rotated)+len(r.registry.deleted) != 0 {
				t.Error("something was read or written for somebody who may not manage the project")
			}
			if len(r.authz.asked) != 1 || r.authz.asked[0] != domain.PermManageProject || r.authz.project[0] != r.project {
				t.Errorf("asked %v for %v, want managing this project", r.authz.asked, r.authz.project)
			}
		})
	}
}

func TestATokenOrAWebhookCanNeverManageAWebhook(t *testing.T) {
	for name, who := range map[string]domain.Identity{
		"a token":   {TokenID: uuid.New(), OrgID: uuid.New()},
		"a webhook": {WebhookID: uuid.New(), OrgID: uuid.New()},
	} {
		r := newManagerRig(t)
		r.authz.allowed = true

		_, err := r.manager.Create(context.Background(), who, r.project)

		if !errors.Is(err, domain.ErrForbidden) {
			t.Errorf("%s: got %v, want ErrForbidden", name, err)
		}
		if len(r.authz.asked) != 0 {
			t.Errorf("%s: the authorizer was asked: it says yes to a thing that must never be allowed", name)
		}
	}
}

// --- Create ----------------------------------------------------------------------------------

func TestCreatingAWebhookGivesTheSecretOnceAndStoresItWithItsEntry(t *testing.T) {
	r := newManagerRig(t)

	created, err := r.manager.Create(context.Background(), r.who, r.project)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if !strings.HasPrefix(created.Secret, domain.WebhookSecretPrefix) {
		t.Errorf("secret %q is not a webhook secret", created.Secret)
	}
	if len(r.registry.created) != 1 || r.registry.secrets[0] != created.Secret {
		t.Fatalf("the registry holds %d webhooks and secret %q: what is stored must be what is returned", len(r.registry.created), r.registry.secrets)
	}
	hook := r.registry.created[0]
	if hook.OrgID != r.who.OrgID || hook.ProjectID != r.project || hook.ID == uuid.Nil || hook.CreatedBy == nil || *hook.CreatedBy != r.who.UserID {
		t.Errorf("stored %+v", hook)
	}
	if created.Webhook.ID != hook.ID || !created.Webhook.CreatedAt.Equal(theMoment) || created.Webhook.RotatedAt != nil {
		t.Errorf("returned %+v", created.Webhook)
	}
	entry := r.registry.entries[0]
	if entry.Action != domain.ActionWebhookCreated || entry.ActorID == nil || *entry.ActorID != r.who.UserID {
		t.Errorf("entry %s by %v", entry.Action, entry.ActorID)
	}
}

func TestACreationOnAProjectThatHasAWebhookIsAConflict(t *testing.T) {
	r := newManagerRig(t)
	r.registry.createErr = domain.ErrWebhookExists

	if _, err := r.manager.Create(context.Background(), r.who, r.project); !errors.Is(err, domain.ErrWebhookExists) {
		t.Errorf("got %v, want ErrWebhookExists", err)
	}
}

func TestASourceOfRandomnessThatFailsMakesNoWebhook(t *testing.T) {
	r := newManagerRig(t)
	r.manager = webhook.NewManager(r.registry, r.authz, clock{at: theMoment}, bytes.NewReader([]byte{1, 2}), slog.New(slog.NewTextHandler(io.Discard, nil)))

	if _, err := r.manager.Create(context.Background(), r.who, r.project); err == nil {
		t.Fatal("a webhook was made with a secret that is mostly zeros")
	}
	if len(r.registry.created) != 0 {
		t.Error("a webhook was written")
	}
}

// --- Rotate and delete -------------------------------------------------------------------------

func TestRotatingReplacesTheSecretAndWritesItsEntry(t *testing.T) {
	r := newManagerRig(t)
	hook := existing(r)

	rotated, err := r.manager.Rotate(context.Background(), r.who, r.project)
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}

	if len(r.registry.rotated) != 1 || r.registry.rotated[0].ID != hook.ID {
		t.Fatalf("rotated %v, want the project's webhook", r.registry.rotated)
	}
	if !strings.HasPrefix(rotated.Secret, domain.WebhookSecretPrefix) || r.registry.secrets[0] != rotated.Secret {
		t.Error("the secret that was stored is not the one returned")
	}
	if rotated.Webhook.ID != hook.ID || rotated.Webhook.RotatedAt == nil || !rotated.Webhook.RotatedAt.Equal(theMoment) {
		t.Errorf("returned %+v: the webhook keeps its identity, and says when its secret changed", rotated.Webhook)
	}
	if e := r.registry.entries[0]; e.Action != domain.ActionWebhookRotated || e.TargetID == nil || *e.TargetID != hook.ID {
		t.Errorf("entry %s on %v", e.Action, e.TargetID)
	}
}

func TestRotatingOrDeletingWhatDoesNotExistIsNotFound(t *testing.T) {
	r := newManagerRig(t)
	if _, err := r.manager.Rotate(context.Background(), r.who, r.project); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("rotate: got %v, want ErrNotFound", err)
	}
	if err := r.manager.Delete(context.Background(), r.who, r.project); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("delete: got %v, want ErrNotFound", err)
	}
	if len(r.registry.rotated)+len(r.registry.deleted) != 0 {
		t.Error("something was written")
	}
}

func TestDeletingRemovesTheWebhookAndWritesItsEntry(t *testing.T) {
	r := newManagerRig(t)
	hook := existing(r)

	if err := r.manager.Delete(context.Background(), r.who, r.project); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(r.registry.deleted) != 1 || r.registry.deleted[0].ID != hook.ID {
		t.Errorf("deleted %v", r.registry.deleted)
	}
	if e := r.registry.entries[0]; e.Action != domain.ActionWebhookDeleted {
		t.Errorf("entry %s", e.Action)
	}
}

// --- What is shown ------------------------------------------------------------------------------

func TestGetShowsTheWebhookAndNothingThatCouldBeTheSecret(t *testing.T) {
	r := newManagerRig(t)
	hook := existing(r)

	got, found, err := r.manager.Get(context.Background(), r.who, r.project)
	if err != nil || !found || got.ID != hook.ID {
		t.Fatalf("got %+v found=%v err=%v", got, found, err)
	}

	r.registry.hook = nil
	if _, found, err := r.manager.Get(context.Background(), r.who, r.project); err != nil || found {
		t.Errorf("a project with no webhook: found=%v err=%v", found, err)
	}
}

func TestNeitherALogNorAnEntryEverHoldsTheSecret(t *testing.T) {
	r := newManagerRig(t)
	created, err := r.manager.Create(context.Background(), r.who, r.project)
	if err != nil {
		t.Fatal(err)
	}
	existing(r)
	rotated, err := r.manager.Rotate(context.Background(), r.who, r.project)
	if err != nil {
		t.Fatal(err)
	}

	for _, secret := range []string{created.Secret, rotated.Secret} {
		if strings.Contains(r.logs.String(), secret) {
			t.Error("the log holds a secret")
		}
		for _, entry := range r.registry.entries {
			for k, v := range entry.Details {
				if strings.Contains(k+v, secret) {
					t.Errorf("an entry holds a secret in %q", k)
				}
			}
		}
	}
	if created.Secret == rotated.Secret {
		t.Error("a rotation gave the same secret")
	}
}

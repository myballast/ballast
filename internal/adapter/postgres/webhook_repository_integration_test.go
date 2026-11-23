//go:build integration

package postgres_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/adapter/postgres"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/infra/crypto"
	"github.com/youssefmahersi/ballast/internal/usecase/authz"
	"github.com/youssefmahersi/ballast/internal/usecase/run"
	"github.com/youssefmahersi/ballast/internal/usecase/webhook"
)

// What the database holds for a project's webhook, against a real one. These hold in
// place:
//
//   - the secret is stored sealed, and bound to its organization and project: moved into
//     another row it no longer opens;
//   - a webhook and its entry are one fact, for the three things an administrator does;
//   - a delivery is remembered once, and what decides it is the key of the row: twenty
//     copies of one request arriving together yield one winner;
//   - nothing crosses an organization.
//
// Written before the code, which is why they do not compile yet.

var (
	_ webhook.Registry   = (*postgres.WebhookRepository)(nil)
	_ webhook.Secrets    = (*postgres.WebhookRepository)(nil)
	_ webhook.Deliveries = (*postgres.WebhookRepository)(nil)
	_ authz.Webhooks     = (*postgres.WebhookRepository)(nil)
	_ run.Repository     = (*postgres.RunRepository)(nil)
)

func webhookRepo(t *testing.T, f fixture) *postgres.WebhookRepository {
	t.Helper()
	cipher, err := crypto.NewCipher([]byte(strings.Repeat("w", crypto.KeyLength)))
	if err != nil {
		t.Fatal(err)
	}
	return postgres.NewWebhookRepository(f.db, cipher)
}

// aNewWebhook is a webhook for a project that has none yet: the fixture's projects each
// have one, so these tests delete it first.
func freshProject(ctx context.Context, t *testing.T, f fixture, org uuid.UUID) (who, domain.Webhook) {
	t.Helper()

	w := whoIn(ctx, t, f, org)
	if err := exec(ctx, f, org, "DELETE FROM project_webhook WHERE project_id = $1", w.project); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	return w, domain.Webhook{ID: uuid.New(), OrgID: org, ProjectID: w.project, CreatedBy: &w.user, CreatedAt: now}
}

const theSecret = "blw_0123456789abcdefghijklmnopqrstuvwxyzABCDEFG"

func TestAWebhookIsStoredSealedAndReadBackOpen(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	repo := webhookRepo(t, f)
	w, hook := freshProject(ctx, t, f, f.orgA)

	if err := repo.Create(ctx, hook, theSecret, domain.WebhookCreatedEntry(&w.user, hook, hook.CreatedAt)); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, secret, found, err := repo.SecretOf(ctx, f.orgA, w.project)
	if err != nil || !found || secret != theSecret || got.ID != hook.ID {
		t.Fatalf("SecretOf: %+v %q found=%v err=%v", got, secret, found, err)
	}

	// What is in the column is not the secret, nor contains it.
	var raw []byte
	if err := f.db.InTenantTx(ctx, f.orgA, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT secret_encrypted FROM project_webhook WHERE id = $1", hook.ID).Scan(&raw)
	}); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(theSecret)) || bytes.Contains(raw, []byte("0123456789abcdef")) {
		t.Error("the secret is in the column in the clear")
	}
}

func TestASealedSecretMovedToAnotherRowDoesNotOpen(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	repo := webhookRepo(t, f)
	wa, hookA := freshProject(ctx, t, f, f.orgA)
	if err := repo.Create(ctx, hookA, theSecret, domain.WebhookCreatedEntry(&wa.user, hookA, hookA.CreatedAt)); err != nil {
		t.Fatal(err)
	}
	// Somebody who can write the table, and holds no key, copies it into the row of
	// another organization's webhook, to read it back as theirs.
	var sealed []byte
	if err := f.db.InTenantTx(ctx, f.orgA, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT secret_encrypted FROM project_webhook WHERE id = $1", hookA.ID).Scan(&sealed)
	}); err != nil {
		t.Fatal(err)
	}
	wb := whoIn(ctx, t, f, f.orgB)
	if err := exec(ctx, f, f.orgB, "UPDATE project_webhook SET secret_encrypted = $2 WHERE project_id = $1", wb.project, sealed); err != nil {
		t.Fatal(err)
	}

	_, secret, found, err := repo.SecretOf(ctx, f.orgB, wb.project)

	if err == nil || secret != "" || found {
		t.Errorf("a secret sealed for another project opened: %q found=%v err=%v", secret, found, err)
	}
}

func TestAProjectWithNoWebhookHasNoSecretAndNothingCrossesAnOrganization(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	repo := webhookRepo(t, f)
	b := whoIn(ctx, t, f, f.orgB)

	// Asked under the wrong organization, as an address with another org in it would.
	if _, _, found, err := repo.SecretOf(ctx, f.orgA, b.project); err != nil || found {
		t.Errorf("another organization's project: found=%v err=%v", found, err)
	}
	if _, found, err := repo.ByProject(ctx, f.orgA, b.project); err != nil || found {
		t.Errorf("ByProject across organizations: found=%v err=%v", found, err)
	}
	hook := webhookRowsOf(ctx, t, f, f.orgB).webhook
	if _, found, err := repo.ByID(ctx, f.orgA, hook); err != nil || found {
		t.Errorf("ByID across organizations: found=%v err=%v", found, err)
	}
}

func TestMakingAWebhookTwiceForAProjectIsAConflictAndWritesOneEntry(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	repo := webhookRepo(t, f)
	w, hook := freshProject(ctx, t, f, f.orgA)

	if err := repo.Create(ctx, hook, theSecret, domain.WebhookCreatedEntry(&w.user, hook, hook.CreatedAt)); err != nil {
		t.Fatal(err)
	}
	again := hook
	again.ID = uuid.New()
	err := repo.Create(ctx, again, theSecret, domain.WebhookCreatedEntry(&w.user, again, again.CreatedAt))

	if !errors.Is(err, domain.ErrWebhookExists) {
		t.Fatalf("got %v, want ErrWebhookExists", err)
	}
	if n := len(journal(ctx, t, f, f.orgA, domain.ActionWebhookCreated)); n != 1 {
		t.Errorf("%d entries, want one: the refused creation must leave none", n)
	}
}

// A webhook and its entry are one fact. An entry that the database refuses — here, one
// for another organization, which row-level security will not take — takes the webhook
// with it.
func TestAWebhookWhoseEntryCannotBeWrittenDoesNotExist(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	repo := webhookRepo(t, f)
	w, hook := freshProject(ctx, t, f, f.orgA)
	elsewhere := hook
	elsewhere.OrgID = f.orgB

	err := repo.Create(ctx, hook, theSecret, domain.WebhookCreatedEntry(&w.user, elsewhere, hook.CreatedAt))

	if err == nil {
		t.Fatal("the creation went through with an entry that could not be written")
	}
	if _, found, _ := repo.ByProject(ctx, f.orgA, w.project); found {
		t.Error("the webhook exists although its entry was refused")
	}
}

func TestRotatingReplacesTheSecretAndKeepsTheWebhook(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	repo := webhookRepo(t, f)
	w, hook := freshProject(ctx, t, f, f.orgA)
	if err := repo.Create(ctx, hook, theSecret, domain.WebhookCreatedEntry(&w.user, hook, hook.CreatedAt)); err != nil {
		t.Fatal(err)
	}

	at := hook.CreatedAt.Add(time.Hour)
	hook.RotatedAt = &at
	if err := repo.Rotate(ctx, hook, "blw_a-new-secret-0123456789abcdefghijklmnopq", at, domain.WebhookRotatedEntry(&w.user, hook, at)); err != nil {
		t.Fatalf("Rotate: %v", err)
	}

	got, secret, found, err := repo.SecretOf(ctx, f.orgA, w.project)
	if err != nil || !found || secret != "blw_a-new-secret-0123456789abcdefghijklmnopq" {
		t.Fatalf("after rotating: %q found=%v err=%v", secret, found, err)
	}
	if got.ID != hook.ID || got.RotatedAt == nil || !got.RotatedAt.Equal(at) {
		t.Errorf("the webhook is %+v: it keeps its identity and says when it was rotated", got)
	}
	if n := len(journal(ctx, t, f, f.orgA, domain.ActionWebhookRotated)); n != 1 {
		t.Errorf("%d rotation entries, want one", n)
	}

	gone := hook
	gone.ID = uuid.New()
	if err := repo.Rotate(ctx, gone, theSecret, at, domain.WebhookRotatedEntry(&w.user, gone, at)); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("rotating a webhook that is not there: %v, want ErrNotFound", err)
	}
	if n := len(journal(ctx, t, f, f.orgA, domain.ActionWebhookRotated)); n != 1 {
		t.Errorf("a refused rotation wrote an entry: %d", n)
	}
}

func TestDeletingRemovesTheWebhookAndKeepsWhatItDid(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	repo := webhookRepo(t, f)
	w, hook := freshProject(ctx, t, f, f.orgA)
	if err := repo.Create(ctx, hook, theSecret, domain.WebhookCreatedEntry(&w.user, hook, hook.CreatedAt)); err != nil {
		t.Fatal(err)
	}

	if err := repo.Delete(ctx, hook, domain.WebhookDeletedEntry(&w.user, hook, hook.CreatedAt)); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, found, _ := repo.ByID(ctx, f.orgA, hook.ID); found {
		t.Error("the webhook is still there")
	}
	if n := len(journal(ctx, t, f, f.orgA, domain.ActionWebhookDeleted)); n != 1 {
		t.Errorf("%d deletion entries, want one", n)
	}
	if err := repo.Delete(ctx, hook, domain.WebhookDeletedEntry(&w.user, hook, hook.CreatedAt)); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("deleting twice: %v, want ErrNotFound", err)
	}
}

// --- Deliveries ------------------------------------------------------------------------------

func TestADeliveryIsServedOnce(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	repo := webhookRepo(t, f)
	w := whoIn(ctx, t, f, f.orgA)
	now := time.Now().UTC()

	first, err := repo.Remember(ctx, f.orgA, w.project, "delivery-aaaaaaaaaaaa", now, now.Add(-10*time.Minute))
	if err != nil || !first {
		t.Fatalf("the first time: %v %v", first, err)
	}
	second, err := repo.Remember(ctx, f.orgA, w.project, "delivery-aaaaaaaaaaaa", now, now.Add(-10*time.Minute))
	if err != nil || second {
		t.Errorf("the second time: %v %v, want it refused", second, err)
	}
}

// What decides is the key of the row. Twenty copies of one request arriving together
// are twenty transactions that all find the delivery absent; one of them wins.
func TestTwentyCopiesOfOneRequestYieldOneWinner(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	repo := webhookRepo(t, f)
	w := whoIn(ctx, t, f, f.orgA)
	now := time.Now().UTC()

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		wins int
	)
	start := make(chan struct{})
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			fresh, err := repo.Remember(ctx, f.orgA, w.project, "delivery-race-0000000", now, now.Add(-10*time.Minute))
			if err != nil {
				t.Errorf("Remember: %v", err)
				return
			}
			if fresh {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()

	if wins != 1 {
		t.Errorf("%d winners among 20 copies, want exactly one", wins)
	}
}

func TestDeliveriesPastTheRetentionAreForgotten(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	repo := webhookRepo(t, f)
	w := whoIn(ctx, t, f, f.orgA)
	now := time.Now().UTC()

	if _, err := repo.Remember(ctx, f.orgA, w.project, "delivery-old-00000000", now.Add(-time.Hour), now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	// Remembering another one, with the retention applied, forgets the old.
	if _, err := repo.Remember(ctx, f.orgA, w.project, "delivery-new-00000000", now, now.Add(-10*time.Minute)); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := f.db.InTenantTx(ctx, f.orgA, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM webhook_delivery WHERE delivery_id = 'delivery-old-00000000'").Scan(&count)
	}); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Error("a delivery an hour old is still remembered")
	}
}

func TestOnlyTheOrganizationsOwnDeliveriesAreForgotten(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	repo := webhookRepo(t, f)
	a, b := whoIn(ctx, t, f, f.orgA), whoIn(ctx, t, f, f.orgB)
	now := time.Now().UTC()

	if _, err := repo.Remember(ctx, f.orgB, b.project, "delivery-of-b-0000000", now.Add(-time.Hour), now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Remember(ctx, f.orgA, a.project, "delivery-of-a-0000000", now, now.Add(-10*time.Minute)); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := f.db.InTenantTx(ctx, f.orgB, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM webhook_delivery WHERE delivery_id = 'delivery-of-b-0000000'").Scan(&count)
	}); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Error("one organization's work forgot another's deliveries")
	}
}

// --- Runs and the journal -----------------------------------------------------------------------

func TestARunLaunchedByAWebhookRoundTripsAndIsCountedByItself(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	runs := postgres.NewRunRepository(f.db)
	r := ciRowsOf(ctx, t, f, f.orgA)
	w := webhookRowsOf(ctx, t, f, f.orgA)
	long := time.Now().Add(-20 * time.Minute)

	if err := exec(ctx, f, f.orgA,
		"UPDATE run SET triggered_by_webhook = $2, created_at = $3 WHERE id = $1", r.run, w.webhook, long); err != nil {
		t.Fatalf("stage the run: %v", err)
	}

	got, err := runs.ByID(ctx, f.orgA, r.run)
	if err != nil {
		t.Fatal(err)
	}
	if got.TriggeredByWebhook == nil || *got.TriggeredByWebhook != w.webhook || got.TriggeredBy != nil || got.TriggeredByToken != nil {
		t.Errorf("the run says %v / %v / %v", got.TriggeredBy, got.TriggeredByToken, got.TriggeredByWebhook)
	}

	since := time.Now().Add(-time.Hour)
	counted, err := runs.TriggeredByWebhookSince(ctx, f.orgA, w.webhook, since)
	if err != nil || counted.ByWebhook != 1 || counted.ByOrg < 1 {
		t.Errorf("counted %+v err=%v, want one by this webhook", counted, err)
	}
	other, err := runs.TriggeredByWebhookSince(ctx, f.orgA, uuid.New(), since)
	if err != nil || other.ByWebhook != 0 {
		t.Errorf("another webhook was counted %+v err=%v", other, err)
	}
	oldest, found, err := runs.OldestByWebhookSince(ctx, f.orgA, w.webhook, since)
	if err != nil || !found || !oldest.Equal(long.UTC()) && oldest.Sub(long).Abs() > time.Second {
		t.Errorf("oldest %s found=%v err=%v, want about %s", oldest, found, err, long)
	}
	if _, found, _ := runs.OldestByWebhookSince(ctx, f.orgA, uuid.New(), since); found {
		t.Error("another webhook has a window")
	}
}

func TestAnEntryByAWebhookRoundTrips(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	w := webhookRowsOf(ctx, t, f, f.orgA)
	journalRepo := postgres.NewAuditRepository(f.db)

	entry := domain.Allow(f.orgA, nil, domain.ActionRunQueued, domain.TargetRun, time.Now().UTC()).
		By(domain.Actor{WebhookID: &w.webhook}).InProject(w.project)
	if err := journalRepo.Record(ctx, entry); err != nil {
		t.Fatalf("Record: %v", err)
	}

	entries, err := journalRepo.InProject(ctx, f.orgA, w.project, 50)
	if err != nil {
		t.Fatal(err)
	}
	var seen bool
	for _, e := range entries {
		if e.ActorWebhookID != nil && *e.ActorWebhookID == w.webhook {
			seen = true
			if e.ActorID != nil || e.ActorTokenID != nil {
				t.Error("the entry names a person or a token as well")
			}
		}
	}
	if !seen {
		t.Error("the entry does not name the webhook after a round trip")
	}
}

//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// A project's webhook is a signing secret, and the record of what was served with it.
// What these tests hold in place is what the schema promises, so that nothing above it
// has to remember:
//
//   - a project has one webhook, and the secret in it is sealed: the column is bytes
//     the application cannot read back as text, and may not be empty;
//   - a delivery is remembered once per project, so a replay cannot be served twice
//     and a second request racing the first loses on the key and not on a check;
//   - a run or an entry names a person, a token or a webhook, never two of them, and
//     keeps naming the webhook when it is deleted or rotated: the record of what a
//     credential did is not the credential.
//
// Written before the migration, which is why they do not pass yet.

type webhookRows struct {
	project, webhook, run uuid.UUID
}

func webhookRowsOf(ctx context.Context, t *testing.T, f fixture, org uuid.UUID) webhookRows {
	t.Helper()

	var r webhookRows
	err := f.db.InTenantTx(ctx, org, func(tx pgx.Tx) error {
		for query, dest := range map[string]*uuid.UUID{
			"SELECT id FROM project         WHERE org_id = $1": &r.project,
			"SELECT id FROM project_webhook WHERE org_id = $1": &r.webhook,
			"SELECT id FROM run             WHERE org_id = $1": &r.run,
		} {
			if err := tx.QueryRow(ctx, query, org).Scan(dest); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read the fixture's rows of %s: %v", org, err)
	}
	return r
}

func TestAProjectHasOneWebhook(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	r := webhookRowsOf(ctx, t, f, f.orgA)

	err := exec(ctx, f, f.orgA, "INSERT INTO project_webhook (org_id, project_id, secret_encrypted) VALUES ($1, $2, $3)",
		f.orgA, r.project, []byte{1})
	if sqlState(err) != "23505" {
		t.Errorf("a second webhook for a project: %v, want a unique violation", err)
	}
}

func TestASecretThatIsEmptyIsRefused(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	r := webhookRowsOf(ctx, t, f, f.orgA)

	err := exec(ctx, f, f.orgA, "UPDATE project_webhook SET secret_encrypted = $2 WHERE id = $1", r.webhook, []byte{})
	if sqlState(err) != "23514" {
		t.Errorf("an empty sealed secret: %v, want a check violation", err)
	}
}

func TestAWebhookCannotBelongToAProjectOfAnotherOrganization(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	b := webhookRowsOf(ctx, t, f, f.orgB)

	// The row is org A's by its own org_id, so row-level security lets it be written;
	// the composite key is what says the project is not.
	err := exec(ctx, f, f.orgA, "DELETE FROM project_webhook WHERE org_id = $1", f.orgA)
	if err != nil {
		t.Fatal(err)
	}
	err = exec(ctx, f, f.orgA, "INSERT INTO project_webhook (org_id, project_id, secret_encrypted) VALUES ($1, $2, $3)",
		f.orgA, b.project, []byte{1})
	if sqlState(err) != "23503" {
		t.Errorf("a webhook for another organization's project: %v, want a foreign key violation", err)
	}
}

func TestADeliveryIsRememberedOncePerProject(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	r := webhookRowsOf(ctx, t, f, f.orgA)

	const insert = "INSERT INTO webhook_delivery (org_id, project_id, delivery_id) VALUES ($1, $2, $3)"
	if err := exec(ctx, f, f.orgA, insert, f.orgA, r.project, "another-delivery-0001"); err != nil {
		t.Fatalf("a new delivery was refused: %v", err)
	}
	err := exec(ctx, f, f.orgA, insert, f.orgA, r.project, "another-delivery-0001")
	if sqlState(err) != "23505" {
		t.Errorf("the same delivery twice: %v, want a unique violation", err)
	}
}

func TestADeliveryIdentifierIsTheShapeTheHeaderHas(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	r := webhookRowsOf(ctx, t, f, f.orgA)

	for name, id := range map[string]string{
		"too short": "abc",
		"too long":  "a123456789012345678901234567890123456789012345678901234567890123456789",
		"a space":   "delivery 0123456789",
		"a newline": "delivery-012345678\n",
		"a slash":   "delivery/0123456789",
		"non ascii": "delivery-é012345678",
	} {
		err := exec(ctx, f, f.orgA, "INSERT INTO webhook_delivery (org_id, project_id, delivery_id) VALUES ($1, $2, $3)",
			f.orgA, r.project, id)
		if sqlState(err) != "23514" {
			t.Errorf("%s: %v, want a check violation", name, err)
		}
	}
}

func TestTheSameDeliveryMayBeServedByAnotherProject(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)

	var other uuid.UUID
	err := f.db.InTenantTx(ctx, f.orgA, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO project (org_id, name, slug) VALUES ($1, 'other', 'other') RETURNING id`, f.orgA).Scan(&other)
	})
	if err != nil {
		t.Fatalf("make a second project: %v", err)
	}
	if err := exec(ctx, f, f.orgA, "INSERT INTO webhook_delivery (org_id, project_id, delivery_id) VALUES ($1, $2, 'delivery-0123456789')",
		f.orgA, other); err != nil {
		t.Errorf("a delivery of another project was refused: %v", err)
	}
}

func TestDeliveriesOfADeletedProjectGoWithIt(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	r := webhookRowsOf(ctx, t, f, f.orgA)

	if err := exec(ctx, f, f.orgA, "DELETE FROM project WHERE id = $1", r.project); err != nil {
		t.Fatalf("delete the project: %v", err)
	}
	var webhooks, deliveries int
	err := f.db.InTenantTx(ctx, f.orgA, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM project_webhook").Scan(&webhooks); err != nil {
			return err
		}
		return tx.QueryRow(ctx, "SELECT count(*) FROM webhook_delivery").Scan(&deliveries)
	})
	if err != nil {
		t.Fatal(err)
	}
	if webhooks != 0 || deliveries != 0 {
		t.Errorf("%d webhooks and %d deliveries survive their project", webhooks, deliveries)
	}
}

// --- run.triggered_by_webhook --------------------------------------------------------------

func TestARunNamesAPersonATokenOrAWebhookAndNeverTwo(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	r := ciRowsOf(ctx, t, f, f.orgA)
	w := webhookRowsOf(ctx, t, f, f.orgA)

	for name, c := range map[string]struct {
		set  string
		args []any
	}{
		"a webhook alone": {"triggered_by = NULL, triggered_by_token = NULL, triggered_by_webhook = $2", []any{f.orgA, w.webhook}},
		"a person alone":  {"triggered_by = $2, triggered_by_token = NULL, triggered_by_webhook = NULL", []any{f.orgA, r.user}},
		"a token alone":   {"triggered_by = NULL, triggered_by_token = $2, triggered_by_webhook = NULL", []any{f.orgA, r.token}},
		"nobody":          {"triggered_by = NULL, triggered_by_token = NULL, triggered_by_webhook = NULL", []any{f.orgA}},
	} {
		if err := exec(ctx, f, f.orgA, "UPDATE run SET "+c.set+" WHERE org_id = $1", c.args...); err != nil {
			t.Errorf("%s was refused: %v", name, err)
		}
	}

	for name, c := range map[string]struct {
		set  string
		args []any
	}{
		"a webhook and a person": {"triggered_by = $2, triggered_by_webhook = $3, triggered_by_token = NULL", []any{f.orgA, r.user, w.webhook}},
		"a webhook and a token":  {"triggered_by_token = $2, triggered_by_webhook = $3, triggered_by = NULL", []any{f.orgA, r.token, w.webhook}},
		"a person and a token":   {"triggered_by = $2, triggered_by_token = $3, triggered_by_webhook = NULL", []any{f.orgA, r.user, r.token}},
	} {
		err := exec(ctx, f, f.orgA, "UPDATE run SET "+c.set+" WHERE org_id = $1", c.args...)
		if sqlState(err) != "23514" {
			t.Errorf("%s: %v, want a check violation", name, err)
		}
	}
}

// The record of who launched a run is not the credential. A webhook that is deleted, or
// whose project goes, leaves the runs saying it launched them.
func TestARunKeepsNamingAWebhookThatIsGone(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	w := webhookRowsOf(ctx, t, f, f.orgA)

	if err := exec(ctx, f, f.orgA, "UPDATE run SET triggered_by_webhook = $2 WHERE id = $1", w.run, w.webhook); err != nil {
		t.Fatalf("name the webhook: %v", err)
	}
	if err := exec(ctx, f, f.orgA, "DELETE FROM project_webhook WHERE id = $1", w.webhook); err != nil {
		t.Fatalf("delete the webhook: %v", err)
	}

	var named *uuid.UUID
	err := f.db.InTenantTx(ctx, f.orgA, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT triggered_by_webhook FROM run WHERE id = $1", w.run).Scan(&named)
	})
	if err != nil {
		t.Fatalf("read the run: %v", err)
	}
	if named == nil || *named != w.webhook {
		t.Errorf("the run says %v, want it to keep naming the webhook %s: cleared, it would read as Ballast acting alone", named, w.webhook)
	}
}

func TestTheRunsOfAWebhookAreIndexedForTheLimit(t *testing.T) {
	ctx, conn := connect(t)

	var columns, predicate string
	err := conn.QueryRow(ctx, `
		SELECT array_to_string(array_agg(a.attname ORDER BY k.ord), ','), coalesce(pg_get_expr(i.indpred, i.indrelid), '')
		FROM pg_index i
		JOIN pg_class c ON c.oid = i.indexrelid
		JOIN LATERAL unnest(i.indkey) WITH ORDINALITY AS k(attnum, ord) ON true
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = k.attnum
		WHERE c.relname = 'run_org_webhook_created_idx'
		GROUP BY i.indexrelid, i.indpred, i.indrelid`).Scan(&columns, &predicate)
	if err != nil {
		t.Fatalf("read the index: %v", err)
	}
	if columns != "org_id,triggered_by_webhook,created_at" {
		t.Errorf("the index covers %q, want org_id first, then the webhook and the date", columns)
	}
	if predicate == "" {
		t.Error("the index covers every run, though most have no webhook")
	}
}

// --- audit_log.actor_webhook_id -------------------------------------------------------------

func TestAnEntryNamesOneKindOfAuthorAtMost(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	r := ciRowsOf(ctx, t, f, f.orgA)
	w := webhookRowsOf(ctx, t, f, f.orgA)

	const insert = `
		INSERT INTO audit_log (org_id, actor_id, actor_token_id, actor_webhook_id, action, outcome, target_type)
		VALUES ($1, $2, $3, $4, 'run.queued', 'allowed', 'run')`

	for name, args := range map[string][]any{
		"a webhook":      {f.orgA, nil, nil, w.webhook},
		"a person":       {f.orgA, r.user, nil, nil},
		"a token":        {f.orgA, nil, r.token, nil},
		"Ballast, alone": {f.orgA, nil, nil, nil},
	} {
		if err := exec(ctx, f, f.orgA, insert, args...); err != nil {
			t.Errorf("an entry by %s was refused: %v", name, err)
		}
	}
	for name, args := range map[string][]any{
		"a webhook and a person": {f.orgA, r.user, nil, w.webhook},
		"a webhook and a token":  {f.orgA, nil, r.token, w.webhook},
		"a person and a token":   {f.orgA, r.user, r.token, nil},
	} {
		if sqlState(exec(ctx, f, f.orgA, insert, args...)) != "23514" {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestAnEntryKeepsNamingAWebhookThatIsGone(t *testing.T) {
	ctx := isolationContext(t)
	f := newFixture(ctx, t)
	w := webhookRowsOf(ctx, t, f, f.orgA)

	if err := exec(ctx, f, f.orgA, `
		INSERT INTO audit_log (org_id, actor_webhook_id, action, outcome, target_type, project_id)
		VALUES ($1, $2, 'run.queued', 'allowed', 'run', $3)`, f.orgA, w.webhook, w.project); err != nil {
		t.Fatalf("write the entry: %v", err)
	}
	if err := exec(ctx, f, f.orgA, "DELETE FROM project WHERE id = $1", w.project); err != nil {
		t.Fatalf("delete the project: %v", err)
	}

	var count int
	err := f.db.InTenantTx(ctx, f.orgA, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM audit_log WHERE actor_webhook_id = $1", w.webhook).Scan(&count)
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("%d entries still name the webhook, want 1: deleting a project must not erase what its webhook did", count)
	}
}

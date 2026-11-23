package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/youssefmahersi/ballast/internal/adapter/postgres/sqlc"
	"github.com/youssefmahersi/ballast/internal/domain"
	"github.com/youssefmahersi/ballast/internal/usecase/authz"
	"github.com/youssefmahersi/ballast/internal/usecase/webhook"
)

// Compile-time proof that this repository satisfies the ports that declared what they
// need from it.
var (
	_ webhook.Registry   = (*WebhookRepository)(nil)
	_ webhook.Secrets    = (*WebhookRepository)(nil)
	_ webhook.Deliveries = (*WebhookRepository)(nil)
	_ authz.Webhooks     = (*WebhookRepository)(nil)
)

// WebhookRepository stores a project's webhook: its sealed secret, and the deliveries
// already served.
//
// One type for all the ports, as SecretRepository is: sealing and opening have to agree
// about the associated data, and two files are how they stop agreeing.
type WebhookRepository struct {
	db     *DB
	cipher Cipher
}

// NewWebhookRepository returns a repository over db, sealing with cipher.
func NewWebhookRepository(db *DB, cipher Cipher) *WebhookRepository {
	return &WebhookRepository{db: db, cipher: cipher}
}

// webhookAAD is what a sealed secret is bound to: the organization and the project.
//
// Supplying it again is what opening requires, so a secret copied into another
// project's row no longer opens. Without it, somebody who can write the table and holds
// no key could move a secret into a row of their own and sign requests with it.
func webhookAAD(orgID, projectID uuid.UUID) []byte {
	return []byte("webhook/" + orgID.String() + "/" + projectID.String())
}

func toDomainWebhook(id, orgID, projectID uuid.UUID, createdBy *uuid.UUID, createdAt time.Time, rotatedAt *time.Time) domain.Webhook {
	return domain.Webhook{
		ID: id, OrgID: orgID, ProjectID: projectID,
		CreatedBy: createdBy, CreatedAt: createdAt, RotatedAt: rotatedAt,
	}
}

// ByProject returns the project's webhook, and false when it has none. The sealed
// column is not read.
func (r *WebhookRepository) ByProject(ctx context.Context, orgID, projectID uuid.UUID) (domain.Webhook, bool, error) {
	var (
		hook  domain.Webhook
		found bool
	)
	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		row, err := sqlc.New(tx).GetProjectWebhook(ctx, sqlc.GetProjectWebhookParams{OrgID: orgID, ProjectID: projectID})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read the webhook: %w", err)
		}
		hook, found = toDomainWebhook(row.ID, row.OrgID, row.ProjectID, row.CreatedBy, row.CreatedAt, row.RotatedAt), true
		return nil
	})
	return hook, found, err
}

// ByID returns a webhook under the organization, and false when there is none: another
// organization's is not there from here.
func (r *WebhookRepository) ByID(ctx context.Context, orgID, webhookID uuid.UUID) (domain.Webhook, bool, error) {
	var (
		hook  domain.Webhook
		found bool
	)
	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		row, err := sqlc.New(tx).GetWebhookByID(ctx, sqlc.GetWebhookByIDParams{OrgID: orgID, ID: webhookID})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read the webhook: %w", err)
		}
		hook, found = toDomainWebhook(row.ID, row.OrgID, row.ProjectID, row.CreatedBy, row.CreatedAt, row.RotatedAt), true
		return nil
	})
	return hook, found, err
}

// SecretOf returns the project's webhook and its secret, opened. It is the one method
// that returns a secret, and it is on the port only the receiver holds.
//
// A secret that does not open is an error, and never "not found": a row somebody edited
// is not a project with no webhook, and saying so would read as a refusal of a
// signature when the fault is the database's.
func (r *WebhookRepository) SecretOf(ctx context.Context, orgID, projectID uuid.UUID) (domain.Webhook, string, bool, error) {
	var (
		hook   domain.Webhook
		secret string
		found  bool
	)
	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		row, err := sqlc.New(tx).GetWebhookSecret(ctx, sqlc.GetWebhookSecretParams{OrgID: orgID, ProjectID: projectID})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read the webhook: %w", err)
		}

		opened, err := r.cipher.Open(webhookAAD(orgID, projectID), row.SecretEncrypted)
		if err != nil {
			// Nothing about the value, and nothing about the row.
			return errors.New("the webhook's secret could not be opened")
		}
		hook = toDomainWebhook(row.ID, row.OrgID, row.ProjectID, row.CreatedBy, row.CreatedAt, row.RotatedAt)
		secret, found = opened, true
		return nil
	})
	if err != nil {
		return domain.Webhook{}, "", false, err
	}
	return hook, secret, found, nil
}

// Create writes the webhook, its sealed secret and its entry in one transaction. A
// project has one: a second is domain.ErrWebhookExists.
//
// The secret is sealed before the statement is built, so the plaintext never reaches
// the connection: not as a bound parameter, and so not into any statement log.
func (r *WebhookRepository) Create(ctx context.Context, hook domain.Webhook, secret string, entry domain.AuditEntry) error {
	sealed, err := r.cipher.Seal(webhookAAD(hook.OrgID, hook.ProjectID), secret)
	if err != nil {
		return errors.New("seal the webhook's secret")
	}

	return r.db.InTenantTx(ctx, hook.OrgID, func(tx pgx.Tx) error {
		if _, err := sqlc.New(tx).InsertWebhook(ctx, sqlc.InsertWebhookParams{
			ID: hook.ID, OrgID: hook.OrgID, ProjectID: hook.ProjectID,
			SecretEncrypted: sealed, CreatedBy: hook.CreatedBy, CreatedAt: hook.CreatedAt,
		}); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
				return domain.ErrWebhookExists
			}
			return fmt.Errorf("write the webhook: %w", err)
		}
		return insertAudit(ctx, tx, entry)
	})
}

// Rotate replaces the secret and writes the entry in one transaction. A webhook that is
// not there matches no row: that is a not found, and no entry is written for it.
func (r *WebhookRepository) Rotate(
	ctx context.Context, hook domain.Webhook, secret string, at time.Time, entry domain.AuditEntry,
) error {
	sealed, err := r.cipher.Seal(webhookAAD(hook.OrgID, hook.ProjectID), secret)
	if err != nil {
		return errors.New("seal the webhook's secret")
	}

	return r.db.InTenantTx(ctx, hook.OrgID, func(tx pgx.Tx) error {
		affected, err := sqlc.New(tx).RotateWebhookSecret(ctx, sqlc.RotateWebhookSecretParams{
			OrgID: hook.OrgID, ID: hook.ID, SecretEncrypted: sealed, RotatedAt: &at,
		})
		if err != nil {
			return fmt.Errorf("replace the webhook's secret: %w", err)
		}
		if affected == 0 {
			return domain.ErrNotFound
		}
		return insertAudit(ctx, tx, entry)
	})
}

// Delete removes the webhook and writes the entry in one transaction.
func (r *WebhookRepository) Delete(ctx context.Context, hook domain.Webhook, entry domain.AuditEntry) error {
	return r.db.InTenantTx(ctx, hook.OrgID, func(tx pgx.Tx) error {
		affected, err := sqlc.New(tx).DeleteWebhook(ctx, sqlc.DeleteWebhookParams{OrgID: hook.OrgID, ID: hook.ID})
		if err != nil {
			return fmt.Errorf("delete the webhook: %w", err)
		}
		if affected == 0 {
			return domain.ErrNotFound
		}
		return insertAudit(ctx, tx, entry)
	})
}

// Remember records the delivery and forgets what is past forgetBefore, in one
// transaction. It reports false when the delivery was already there.
//
// What decides is the primary key: the insert does nothing on a conflict, and the number
// of rows it touched is the answer. Two copies of one request arriving together both run
// this, one waits for the other's insert to commit, and finds the row.
func (r *WebhookRepository) Remember(
	ctx context.Context, orgID, projectID uuid.UUID, deliveryID string, at, forgetBefore time.Time,
) (bool, error) {
	var fresh bool

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		q := sqlc.New(tx)

		if _, err := q.ForgetWebhookDeliveries(ctx, sqlc.ForgetWebhookDeliveriesParams{
			OrgID: orgID, ReceivedAt: forgetBefore,
		}); err != nil {
			return fmt.Errorf("forget the old deliveries: %w", err)
		}

		inserted, err := q.RememberWebhookDelivery(ctx, sqlc.RememberWebhookDeliveryParams{
			OrgID: orgID, ProjectID: projectID, DeliveryID: deliveryID, ReceivedAt: at,
		})
		if err != nil {
			return fmt.Errorf("remember the delivery: %w", err)
		}
		fresh = inserted == 1
		return nil
	})
	return fresh, err
}

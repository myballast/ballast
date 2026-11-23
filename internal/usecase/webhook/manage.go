package webhook

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// Manager makes, replaces and removes a project's webhook: what an administrator does
// from the screen. It is separate from Receiver, which serves the requests a tool signs:
// the path every call takes has no business depending on permissions, and the manager
// has no port that returns a secret.
type Manager struct {
	registry Registry
	authz    Authorizer
	clock    Clock
	random   io.Reader
	log      *slog.Logger
}

// NewManager wires the manager. random is where secrets come from: the system's
// randomness in production, and a fixed source in a test.
func NewManager(registry Registry, authz Authorizer, clock Clock, random io.Reader, log *slog.Logger) *Manager {
	return &Manager{registry: registry, authz: authz, clock: clock, random: random, log: log}
}

// Created is the result of making or replacing a secret.
type Created struct {
	Webhook domain.Webhook
	// Secret is the key itself. It exists here and in the sealed column and nowhere
	// else, and it is returned once: nothing can read it back.
	Secret string
}

// Get returns the project's webhook, and false when it has none. It says when the
// webhook was made and its secret last replaced, and nothing that could be the secret.
func (m *Manager) Get(ctx context.Context, who domain.Identity, projectID uuid.UUID) (domain.Webhook, bool, error) {
	if err := m.require(ctx, who, projectID); err != nil {
		return domain.Webhook{}, false, err
	}
	hook, found, err := m.registry.ByProject(ctx, who.OrgID, projectID)
	if err != nil {
		return domain.Webhook{}, false, fmt.Errorf("read the webhook: %w", err)
	}
	return hook, found, nil
}

// Create makes the project's webhook and returns its secret, once.
func (m *Manager) Create(ctx context.Context, who domain.Identity, projectID uuid.UUID) (Created, error) {
	if err := m.require(ctx, who, projectID); err != nil {
		return Created{}, err
	}

	secret, err := domain.NewWebhookSecret(m.random)
	if err != nil {
		return Created{}, fmt.Errorf("make a secret: %w", err)
	}
	now := m.clock.Now()
	hook := domain.Webhook{
		ID: uuid.New(), OrgID: who.OrgID, ProjectID: projectID,
		CreatedBy: &who.UserID, CreatedAt: now,
	}

	if err := m.registry.Create(ctx, hook, secret, domain.WebhookCreatedEntry(&who.UserID, hook, now)); err != nil {
		return Created{}, err
	}

	// The webhook's id and who made it. Never the secret.
	m.log.InfoContext(ctx, "webhook created",
		"org_id", who.OrgID, "project_id", projectID, "webhook_id", hook.ID, "by", who.UserID)
	return Created{Webhook: hook, Secret: secret}, nil
}

// Rotate replaces the project's secret. The old one stops working at once, and the
// address a tool calls does not change.
func (m *Manager) Rotate(ctx context.Context, who domain.Identity, projectID uuid.UUID) (Created, error) {
	if err := m.require(ctx, who, projectID); err != nil {
		return Created{}, err
	}

	hook, found, err := m.registry.ByProject(ctx, who.OrgID, projectID)
	if err != nil {
		return Created{}, fmt.Errorf("read the webhook: %w", err)
	}
	if !found {
		return Created{}, domain.ErrNotFound
	}

	secret, err := domain.NewWebhookSecret(m.random)
	if err != nil {
		return Created{}, fmt.Errorf("make a secret: %w", err)
	}
	now := m.clock.Now()
	hook.RotatedAt = &now

	if err := m.registry.Rotate(ctx, hook, secret, now, domain.WebhookRotatedEntry(&who.UserID, hook, now)); err != nil {
		return Created{}, err
	}

	m.log.InfoContext(ctx, "webhook secret rotated",
		"org_id", who.OrgID, "project_id", projectID, "webhook_id", hook.ID, "by", who.UserID)
	return Created{Webhook: hook, Secret: secret}, nil
}

// Delete removes the project's webhook. A tool that still signs requests is refused
// from the next one.
func (m *Manager) Delete(ctx context.Context, who domain.Identity, projectID uuid.UUID) error {
	if err := m.require(ctx, who, projectID); err != nil {
		return err
	}

	hook, found, err := m.registry.ByProject(ctx, who.OrgID, projectID)
	if err != nil {
		return fmt.Errorf("read the webhook: %w", err)
	}
	if !found {
		return domain.ErrNotFound
	}

	now := m.clock.Now()
	if err := m.registry.Delete(ctx, hook, domain.WebhookDeletedEntry(&who.UserID, hook, now)); err != nil {
		return err
	}

	m.log.InfoContext(ctx, "webhook deleted",
		"org_id", who.OrgID, "project_id", projectID, "webhook_id", hook.ID, "by", who.UserID)
	return nil
}

// require refuses anybody who may not manage the project. A token or a webhook is
// refused before the authorizer is even asked: one that could manage webhooks could make
// a successor and outlive its own removal, whatever the authorizer would say.
func (m *Manager) require(ctx context.Context, who domain.Identity, projectID uuid.UUID) error {
	if who.IsToken() || who.IsWebhook() {
		return domain.ErrForbidden
	}

	allowed, err := m.authz.Can(ctx, who, domain.PermManageProject, projectID)
	if err != nil {
		return fmt.Errorf("check the permission: %w", err)
	}
	if !allowed {
		return domain.ErrForbidden
	}
	return nil
}

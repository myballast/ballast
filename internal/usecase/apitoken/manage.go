package apitoken

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// Manager creates, lists and revokes the tokens of a project: what an
// administrator does from the screen. It is separate from Service, which
// authenticates a request: the path every pipeline call takes has no business
// depending on a journal or on permissions.
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

// CreateCommand is what an administrator asks for.
type CreateCommand struct {
	Name string
	// ExpiresAt is optional. Left empty, the default lifetime applies.
	ExpiresAt *time.Time
}

// Created is the result of creating a token.
type Created struct {
	Token domain.APIToken
	// Secret is the credential itself. It exists here and nowhere else: only its
	// hash is stored, so it cannot be shown again.
	Secret string
}

// Item is a token as the list shows it.
type Item struct {
	Token         domain.APIToken
	CreatedByName string
	// State is read against the clock when the list is asked for, never stored.
	State domain.APITokenState
}

// Create writes a token for the project and returns its secret, once.
//
// The permission is checked before anything is read or validated: somebody who
// may not manage the project is not told what is wrong with a request they may
// not make.
func (m *Manager) Create(
	ctx context.Context, who domain.Identity, projectID uuid.UUID, cmd CreateCommand,
) (Created, error) {
	if err := m.require(ctx, who, projectID); err != nil {
		return Created{}, err
	}

	name, err := domain.ValidateAPITokenName(cmd.Name)
	if err != nil {
		return Created{}, err
	}
	now := m.clock.Now()
	expiresAt, err := domain.ResolveAPITokenExpiry(cmd.ExpiresAt, now)
	if err != nil {
		return Created{}, err
	}

	secret, err := domain.NewAPITokenSecret(m.random)
	if err != nil {
		return Created{}, fmt.Errorf("make a secret: %w", err)
	}

	token := domain.APIToken{
		ID:        uuid.New(),
		OrgID:     who.OrgID,
		ProjectID: projectID,
		Name:      name,
		Prefix:    domain.APITokenDisplayPrefix(secret),
		CreatedBy: &who.UserID,
		ExpiresAt: expiresAt,
		CreatedAt: now,
	}
	entry := domain.APITokenCreatedEntry(&who.UserID, token, now)

	if err := m.registry.Create(ctx, token, domain.HashAPITokenSecret(secret), entry); err != nil {
		return Created{}, err
	}

	// The token's id and who made it. Never the secret, and not its prefix
	// either: the id is enough to follow the entry.
	m.log.InfoContext(ctx, "API token created",
		"org_id", who.OrgID, "project_id", projectID, "token_id", token.ID, "by", who.UserID)

	return Created{Token: token, Secret: secret}, nil
}

// List returns the project's tokens: the ones that still work first, newest
// first, and the revoked ones after them. A list in which a withdrawn token sits
// above a live one is a list somebody reads the wrong line of.
func (m *Manager) List(ctx context.Context, who domain.Identity, projectID uuid.UUID) ([]Item, error) {
	if err := m.require(ctx, who, projectID); err != nil {
		return nil, err
	}

	listed, err := m.registry.List(ctx, who.OrgID, projectID)
	if err != nil {
		return nil, err
	}

	now := m.clock.Now()
	items := make([]Item, 0, len(listed))
	for _, l := range listed {
		items = append(items, Item{Token: l.APIToken, CreatedByName: l.CreatedByName, State: l.State(now)})
	}

	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if revoked := a.State == domain.APITokenRevoked; revoked != (b.State == domain.APITokenRevoked) {
			return !revoked
		}
		return a.Token.CreatedAt.After(b.Token.CreatedAt)
	})

	return items, nil
}

// Revoke withdraws a token of the project. It stops working on the next request.
//
// A token that is not this project's is not found, whichever way it is not: it
// does not exist, it belongs to another project or organization, or it was
// already revoked. One answer for all of them, so that nobody learns what exists
// elsewhere.
func (m *Manager) Revoke(ctx context.Context, who domain.Identity, projectID, tokenID uuid.UUID) error {
	if err := m.require(ctx, who, projectID); err != nil {
		return err
	}

	token, found, err := m.registry.ByID(ctx, who.OrgID, tokenID)
	if err != nil {
		return fmt.Errorf("read the API token: %w", err)
	}
	if !found || token.ProjectID != projectID || token.RevokedAt != nil {
		return domain.ErrNotFound
	}

	now := m.clock.Now()
	if err := m.registry.Revoke(ctx, token, now, domain.APITokenRevokedEntry(&who.UserID, token, now)); err != nil {
		return err
	}

	m.log.InfoContext(ctx, "API token revoked",
		"org_id", who.OrgID, "project_id", projectID, "token_id", tokenID, "by", who.UserID)

	return nil
}

// require refuses anybody who may not manage the project.
//
// A token is refused before the authorizer is even asked: one that could manage
// tokens could mint a successor and outlive its own revocation, whatever the
// authorizer would say.
func (m *Manager) require(ctx context.Context, who domain.Identity, projectID uuid.UUID) error {
	if who.IsToken() {
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

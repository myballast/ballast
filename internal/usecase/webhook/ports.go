package webhook

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// The interfaces below are declared here, by the code that consumes them, and
// implemented elsewhere.

// Secrets reads the signing secret of a project's webhook.
//
// It is the only thing that can return the secret in the clear, and only the receiver
// holds it: the manager's port has no such method, so the process that serves the
// screen has no way to ask for the value it has to be unable to show.
type Secrets interface {
	// SecretOf returns the webhook of a project and its secret, opened, and false when
	// the project has none. Read under the organization, so that another one's
	// project is not found either.
	SecretOf(ctx context.Context, orgID, projectID uuid.UUID) (domain.Webhook, string, bool, error)
}

// Deliveries remembers which deliveries were served.
type Deliveries interface {
	// Remember records the delivery and forgets, in the same transaction, those older
	// than forgetBefore. It reports false when the delivery was already there. What
	// decides is the key of the row, and not a read before the write: two copies
	// arriving together cannot both find it absent.
	Remember(ctx context.Context, orgID, projectID uuid.UUID, deliveryID string, at, forgetBefore time.Time) (bool, error)
}

// Launch is what the receiver asks the run service for.
type Launch struct {
	// Actor is the webhook, and nobody else: no person and no token.
	Actor         domain.Identity
	ScenarioID    uuid.UUID
	EnvironmentID uuid.UUID
	Profile       domain.ProfileKind
	Params        *domain.ProfileParams
	CI            *domain.RunCI
}

// Launcher starts a run. It is the run service, which is where every guardrail is;
// there is no second way in.
type Launcher interface {
	Enqueue(ctx context.Context, launch Launch) (domain.Run, error)
}

// Clock tells the time.
type Clock interface {
	Now() time.Time
}

// Registry is what managing a webhook writes and reads. The secret goes in, sealed by
// the implementation, and never comes out.
type Registry interface {
	// ByProject returns the project's webhook, and false when it has none.
	ByProject(ctx context.Context, orgID, projectID uuid.UUID) (domain.Webhook, bool, error)
	// Create writes the webhook, its sealed secret and its journal entry in one
	// transaction, and refuses with domain.ErrWebhookExists when the project has one.
	Create(ctx context.Context, hook domain.Webhook, secret string, entry domain.AuditEntry) error
	// Rotate replaces the secret and writes the entry in one transaction. It returns
	// domain.ErrNotFound when the project has no webhook.
	Rotate(ctx context.Context, hook domain.Webhook, secret string, at time.Time, entry domain.AuditEntry) error
	// Delete removes the webhook and writes the entry in one transaction. It returns
	// domain.ErrNotFound when there is none.
	Delete(ctx context.Context, hook domain.Webhook, entry domain.AuditEntry) error
}

// Authorizer answers what a caller may do in a project.
type Authorizer interface {
	Can(ctx context.Context, who domain.Identity, perm domain.Permission, projectID uuid.UUID) (bool, error)
}

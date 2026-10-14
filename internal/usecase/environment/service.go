package environment

import (
	"context"
	"log/slog"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// Service orchestrates target environments and their secrets.
type Service struct {
	environments Repository
	secrets      Secrets
	tokens       Tokens
	proof        ProofReader
	journal      AuditLog
	authz        Authorizer
	clock        Clock
	log          *slog.Logger
}

// NewService wires the service.
func NewService(
	environments Repository,
	secrets Secrets,
	tokens Tokens,
	proof ProofReader,
	journal AuditLog,
	authz Authorizer,
	clock Clock,
	log *slog.Logger,
) *Service {
	return &Service{
		environments: environments,
		secrets:      secrets,
		tokens:       tokens,
		proof:        proof,
		journal:      journal,
		authz:        authz,
		clock:        clock,
		log:          log,
	}
}

// InProject lists a project's environments.
func (s *Service) InProject(
	ctx context.Context, who domain.Identity, projectID uuid.UUID,
) ([]domain.Environment, error) {
	if err := s.require(ctx, who, domain.PermViewProject, projectID); err != nil {
		return nil, err
	}

	return s.environments.InProject(ctx, who.OrgID, projectID)
}

// NewEnvironment is an environment to declare.
type NewEnvironment struct {
	ProjectID    uuid.UUID
	Name         string
	BaseURL      string
	MaxVUs       int
	MaxDurationS int
	// Window is optional. Nil means the target takes load at any hour.
	Window *domain.AllowedWindow
}

// Create declares a target.
//
// It arrives unverified, whatever the caller asks for: an environment nobody has
// proved they own is the one thing Ballast must never send traffic at, and a
// creation path able to produce a verified row would be a way around the whole
// control. The token it will have to serve is written now because the column
// cannot be empty; serving it, reading it and approving the result belong to the
// verification.
func (s *Service) Create(
	ctx context.Context, who domain.Identity, request NewEnvironment,
) (domain.Environment, error) {
	if err := s.require(ctx, who, domain.PermManageEnvironments, request.ProjectID); err != nil {
		return domain.Environment{}, err
	}
	if err := validate(request.Name, request.BaseURL, request.MaxVUs, request.MaxDurationS, request.Window); err != nil {
		return domain.Environment{}, err
	}

	token, err := s.tokens.New()
	if err != nil {
		return domain.Environment{}, err
	}

	created, err := s.environments.Create(ctx, who.OrgID, domain.Environment{
		ID:                 uuid.New(),
		ProjectID:          request.ProjectID,
		Name:               strings.TrimSpace(request.Name),
		BaseURL:            request.BaseURL,
		VerificationStatus: domain.VerificationPending,
		MaxVUs:             request.MaxVUs,
		MaxDurationS:       request.MaxDurationS,
		Window:             request.Window,
	}, token, s.clock.Now())
	if err != nil {
		return domain.Environment{}, err
	}

	s.log.InfoContext(ctx, "environment declared",
		"org_id", who.OrgID, "project_id", request.ProjectID,
		"environment_id", created.ID, "base_url", created.BaseURL)

	return created, nil
}

// ByID returns one environment.
func (s *Service) ByID(
	ctx context.Context, who domain.Identity, projectID, id uuid.UUID,
) (domain.Environment, error) {
	return s.read(ctx, who, domain.PermViewProject, projectID, id)
}

// Update is what a caller wants changed. Anything left nil stays as it is.
type Update struct {
	Name         *string
	BaseURL      *string
	MaxVUs       *int
	MaxDurationS *int
	// Window is only read when WindowSet is true, where a nil Window removes the
	// window altogether. The two fields exist because "leave it alone" and
	// "there is no window any more" are different instructions and JSON spells
	// them both as an absent value.
	Window    *domain.AllowedWindow
	WindowSet bool
}

// Update changes an environment.
//
// Pointing it at another host resets the verification. The proof was served by
// one origin and says nothing about the next, so an environment edited from a
// host somebody owns to a host they do not would otherwise stay approved and
// carry load straight there — the exact thing the verification exists to stop.
// The comparison is on the origin, port included, since a proof served on one
// port says nothing about another; editing the path below it leaves the proof
// standing.
func (s *Service) Update(
	ctx context.Context, who domain.Identity, projectID, id uuid.UUID, update Update,
) (domain.Environment, error) {
	current, err := s.read(ctx, who, domain.PermManageEnvironments, projectID, id)
	if err != nil {
		return domain.Environment{}, err
	}

	updated := current
	if update.Name != nil {
		updated.Name = strings.TrimSpace(*update.Name)
	}
	if update.BaseURL != nil {
		updated.BaseURL = *update.BaseURL
	}
	if update.MaxVUs != nil {
		updated.MaxVUs = *update.MaxVUs
	}
	if update.MaxDurationS != nil {
		updated.MaxDurationS = *update.MaxDurationS
	}
	if update.WindowSet {
		updated.Window = update.Window
	}

	if err := validate(updated.Name, updated.BaseURL, updated.MaxVUs, updated.MaxDurationS, updated.Window); err != nil {
		return domain.Environment{}, err
	}

	resetVerification := originOf(updated.BaseURL) != originOf(current.BaseURL)
	if resetVerification {
		updated.VerificationStatus = domain.VerificationPending

		s.log.WarnContext(ctx, "environment retargeted, verification reset",
			"org_id", who.OrgID, "user_id", who.UserID, "environment_id", id,
			"from", current.BaseURL, "to", updated.BaseURL,
			"was", current.VerificationStatus)
	}

	saved, err := s.environments.Save(ctx, who.OrgID, updated, resetVerification)
	if err != nil {
		return domain.Environment{}, err
	}

	s.log.InfoContext(ctx, "environment updated",
		"org_id", who.OrgID, "environment_id", id, "verification_reset", resetVerification)

	return saved, nil
}

// Delete removes an environment that holds no run.
func (s *Service) Delete(
	ctx context.Context, who domain.Identity, projectID, id uuid.UUID,
) error {
	if _, err := s.read(ctx, who, domain.PermManageEnvironments, projectID, id); err != nil {
		return err
	}

	if err := s.environments.Delete(ctx, who.OrgID, id); err != nil {
		return err
	}

	s.log.InfoContext(ctx, "environment deleted",
		"org_id", who.OrgID, "environment_id", id, "by", who.UserID)
	return nil
}

// SecretKeys lists what is set on an environment, which is all anybody gets to
// read back.
//
// It takes the permission to manage environments rather than the one to view the
// project: the name of a key says how the target expects to be authenticated,
// and that is not something to show every reader.
func (s *Service) SecretKeys(
	ctx context.Context, who domain.Identity, projectID, id uuid.UUID,
) ([]domain.Secret, error) {
	if _, err := s.read(ctx, who, domain.PermManageEnvironments, projectID, id); err != nil {
		return nil, err
	}

	return s.secrets.Keys(ctx, who.OrgID, id)
}

// SetSecret stores a credential under key, replacing whatever was there.
//
// The value is validated and handed straight to the repository, which encrypts
// it. Nothing logged from here names it, and nothing returned from here carries
// it: domain.Secret has no field for one.
func (s *Service) SetSecret(
	ctx context.Context, who domain.Identity, projectID, id uuid.UUID, key, value string,
) (domain.Secret, error) {
	if _, err := s.read(ctx, who, domain.PermManageEnvironments, projectID, id); err != nil {
		return domain.Secret{}, err
	}
	if err := domain.ValidateSecretKey(key); err != nil {
		return domain.Secret{}, err
	}
	if err := domain.ValidateSecretValue(value); err != nil {
		return domain.Secret{}, err
	}

	stored, err := s.secrets.Put(ctx, who.OrgID, id, key, value, s.clock.Now())
	if err != nil {
		return domain.Secret{}, err
	}

	s.log.InfoContext(ctx, "secret set",
		"org_id", who.OrgID, "user_id", who.UserID, "environment_id", id, "key", key)

	return stored, nil
}

// RemoveSecret drops a credential.
func (s *Service) RemoveSecret(
	ctx context.Context, who domain.Identity, projectID, id uuid.UUID, key string,
) error {
	if _, err := s.read(ctx, who, domain.PermManageEnvironments, projectID, id); err != nil {
		return err
	}

	if err := s.secrets.Remove(ctx, who.OrgID, id, key); err != nil {
		return err
	}

	s.log.InfoContext(ctx, "secret removed",
		"org_id", who.OrgID, "user_id", who.UserID, "environment_id", id, "key", key)
	return nil
}

// read checks the permission, reads the environment, and checks that it really
// belongs to the project the path named.
//
// The last part is what row-level security cannot do: both rows are the caller's
// organization, and an environment of another project reached through a project
// the caller administers would otherwise pass every check.
func (s *Service) read(
	ctx context.Context, who domain.Identity, perm domain.Permission, projectID, id uuid.UUID,
) (domain.Environment, error) {
	if err := s.require(ctx, who, perm, projectID); err != nil {
		return domain.Environment{}, err
	}

	found, err := s.environments.ByID(ctx, who.OrgID, id)
	if err != nil {
		return domain.Environment{}, err
	}

	if err := domain.RequireSameProject(projectID, found.ProjectID); err != nil {
		// Named as absent rather than refused: the caller asked about a project
		// they may see, and this environment is not in it.
		return domain.Environment{}, domain.ErrNotFound
	}

	return found, nil
}

func (s *Service) require(
	ctx context.Context, who domain.Identity, perm domain.Permission, projectID uuid.UUID,
) error {
	allowed, err := s.authz.Can(ctx, who, perm, projectID)
	if err != nil {
		return err
	}
	if !allowed {
		s.log.InfoContext(ctx, "request refused",
			"org_id", who.OrgID, "user_id", who.UserID,
			"project_id", projectID, "permission", int64(perm))
		return domain.ErrForbidden
	}
	return nil
}

func validate(name, baseURL string, maxVUs, maxDurationS int, window *domain.AllowedWindow) error {
	if err := domain.ValidateEnvironment(name, baseURL, maxVUs, maxDurationS); err != nil {
		return err
	}
	if window == nil {
		return nil
	}
	return window.Validate()
}

// originOf is the part of a base URL a proof of ownership covers: the scheme,
// the host and the port. A URL that cannot be read yields itself, so two
// unreadable URLs compare equal only when they are the same string —
// validation has already refused both by the time this matters.
func originOf(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return strings.ToLower(parsed.Scheme + "://" + parsed.Host)
}

// record writes one line of the journal, and says so loudly when it cannot.
//
// A failure here does not undo what happened: the target was verified, the role
// was changed, and pretending otherwise would be a second lie on top of a
// missing line. What it must never be is silent — a journal with holes nobody
// knows about is worse than one with holes everybody knows about.
//
// The entries that must not be missing at all are not written here: they travel
// into the transaction that performs the action.
func (s *Service) record(ctx context.Context, entry domain.AuditEntry) {
	if err := s.journal.Record(ctx, entry); err != nil {
		s.log.ErrorContext(ctx, "an audit entry could not be recorded",
			"action", entry.Action, "outcome", entry.Outcome,
			"org_id", entry.OrgID, "error", err)
	}
}

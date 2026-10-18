package invitation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// Service creates, lists, revokes and accepts invitations.
type Service struct {
	invitations Repository
	people      People
	memberships Memberships
	authz       Authorizer
	journal     AuditLog
	tokens      Tokens
	clock       Clock
	log         *slog.Logger
}

// NewService wires the service.
func NewService(
	invitations Repository,
	people People,
	memberships Memberships,
	authz Authorizer,
	journal AuditLog,
	tokens Tokens,
	clock Clock,
	log *slog.Logger,
) *Service {
	return &Service{
		invitations: invitations,
		people:      people,
		memberships: memberships,
		authz:       authz,
		journal:     journal,
		tokens:      tokens,
		clock:       clock,
		log:         log,
	}
}

// Created is the result of inviting somebody: either an invitation, with the
// token that is shown once, or the member, when the address already belonged to
// a person of the organization and there was nothing to send.
type Created struct {
	Invitation *domain.Invitation
	// Token is the secret in the link. It exists here and nowhere else: only its
	// hash is stored, so it cannot be shown again.
	Token string
	// Added is the user who was put straight into the project.
	Added *uuid.UUID
}

// Create invites an address to a project with a role.
//
// The caller can give up to their own role and no higher. An address that is
// already a person of the organization is added at once, since an invitation
// would only ask them to prove what Ballast already knows.
func (s *Service) Create(
	ctx context.Context, who domain.Identity, projectID uuid.UUID, email string, role domain.Role,
) (Created, error) {
	if err := s.require(ctx, who, projectID); err != nil {
		return Created{}, err
	}

	address, err := domain.ValidateInvitation(email, role)
	if err != nil {
		return Created{}, err
	}

	own, found, err := s.authz.MembershipOf(ctx, who, projectID)
	if err != nil {
		return Created{}, fmt.Errorf("read the caller's membership: %w", err)
	}
	if !found {
		return Created{}, domain.ErrForbidden
	}
	if !domain.CanGrant(own.Role, role) {
		s.refused(ctx, domain.Refuse(
			who.OrgID, &who.UserID, domain.ActionMemberInvited, domain.TargetInvitation, "role_above_own", s.clock.Now(),
		).InProject(projectID))
		return Created{}, domain.ErrRoleAboveOwn
	}

	now := s.clock.Now()

	existing, isUser, err := s.invitations.UserByEmail(ctx, who.OrgID, address)
	if err != nil {
		return Created{}, err
	}
	if isUser {
		return s.add(ctx, who, projectID, existing, role)
	}

	token, err := s.tokens.New()
	if err != nil {
		return Created{}, fmt.Errorf("make a token: %w", err)
	}

	invitation := domain.Invitation{
		ID:        uuid.New(),
		OrgID:     who.OrgID,
		ProjectID: projectID,
		Email:     address,
		Role:      role,
		InvitedBy: &who.UserID,
		ExpiresAt: now.Add(domain.InvitationTTL),
		CreatedAt: now,
	}
	entry := domain.Allow(
		who.OrgID, &who.UserID, domain.ActionMemberInvited, domain.TargetInvitation, now,
	).InProject(projectID).On(invitation.ID).With("role", string(role))

	if err := s.invitations.Create(ctx, invitation, HashToken(token), entry); err != nil {
		return Created{}, err
	}

	s.log.InfoContext(ctx, "member invited",
		"org_id", who.OrgID, "project_id", projectID, "invitation_id", invitation.ID, "role", role, "by", who.UserID)

	return Created{Invitation: &invitation, Token: token}, nil
}

// add puts an existing person of the organization straight into the project.
func (s *Service) add(
	ctx context.Context, who domain.Identity, projectID, userID uuid.UUID, role domain.Role,
) (Created, error) {
	_, isMember, err := s.authz.MembershipOf(ctx, domain.Identity{UserID: userID, OrgID: who.OrgID}, projectID)
	if err != nil {
		return Created{}, fmt.Errorf("read the membership: %w", err)
	}
	if isMember {
		return Created{}, domain.ErrAlreadyMember
	}

	entry := domain.Allow(
		who.OrgID, &who.UserID, domain.ActionRoleChanged, domain.TargetMembership, s.clock.Now(),
	).InProject(projectID).On(userID).With("role", string(role))

	if err := s.memberships.SetRole(ctx, who.OrgID, projectID, userID, role, entry); err != nil {
		return Created{}, err
	}

	s.log.InfoContext(ctx, "member added",
		"org_id", who.OrgID, "project_id", projectID, "user_id", userID, "role", role, "by", who.UserID)

	return Created{Added: &userID}, nil
}

// Open is an invitation that has been neither used nor revoked, with whether it
// can still be used.
type Open struct {
	domain.Invitation
	State domain.InvitationState
}

// Pending lists the invitations that are still open, the expired ones included:
// they are listed so that they can be revoked or replaced.
func (s *Service) Pending(
	ctx context.Context, who domain.Identity, projectID uuid.UUID,
) ([]Open, error) {
	if err := s.require(ctx, who, projectID); err != nil {
		return nil, err
	}

	found, err := s.invitations.Pending(ctx, who.OrgID, projectID)
	if err != nil {
		return nil, err
	}

	now := s.clock.Now()
	open := make([]Open, 0, len(found))
	for _, invitation := range found {
		open = append(open, Open{Invitation: invitation, State: invitation.State(now)})
	}
	return open, nil
}

// Revoke withdraws an invitation that has not been used.
func (s *Service) Revoke(
	ctx context.Context, who domain.Identity, projectID, id uuid.UUID,
) error {
	if err := s.require(ctx, who, projectID); err != nil {
		return err
	}

	now := s.clock.Now()
	entry := domain.Allow(
		who.OrgID, &who.UserID, domain.ActionInvitationRevoked, domain.TargetInvitation, now,
	).InProject(projectID).On(id)

	if err := s.invitations.Revoke(ctx, who.OrgID, projectID, id, now, entry); err != nil {
		return err
	}

	s.log.InfoContext(ctx, "invitation revoked",
		"org_id", who.OrgID, "project_id", projectID, "invitation_id", id, "by", who.UserID)
	return nil
}

// Describe reads a link. It needs no session: the page has to say who is asking
// before the person it asks has signed in, and the token is what proves they
// were given it.
//
// An unknown token is domain.ErrNotFound. A known one is described whatever
// became of it, with its state, so that the page can say what happened rather
// than that something did.
func (s *Service) Describe(ctx context.Context, token string) (Described, domain.InvitationState, error) {
	found, ok, err := s.invitations.ByTokenHash(ctx, HashToken(token))
	if err != nil {
		return Described{}, "", err
	}
	if !ok {
		return Described{}, "", domain.ErrNotFound
	}
	return found, found.State(s.clock.Now()), nil
}

// Joined is who the person is in the organization that invited them, and the
// project they joined.
type Joined struct {
	domain.Identity
	ProjectID uuid.UUID
}

// Accept makes the signed-in person a member of the project they were invited
// to, and returns who they are in the inviting organization.
//
// The address on the invitation must be the one the person signed in with. The
// link is a credential, but a credential for one address: forwarded to somebody
// else, it does not make them a member.
func (s *Service) Accept(ctx context.Context, who domain.Identity, token string) (Joined, error) {
	found, ok, err := s.invitations.ByTokenHash(ctx, HashToken(token))
	if err != nil {
		return Joined{}, err
	}
	if !ok {
		return Joined{}, domain.ErrNotFound
	}

	now := s.clock.Now()
	if state := found.State(now); state != domain.InvitationPending {
		return Joined{}, fmt.Errorf("%w: %s", domain.ErrInvitationUnusable, state)
	}

	profile, err := s.people.Profile(ctx, who)
	if err != nil {
		return Joined{}, fmt.Errorf("read the invitee: %w", err)
	}

	if !domain.SameAddress(profile.Email, found.Email) {
		s.refused(ctx, domain.Refuse(
			found.OrgID, nil, domain.ActionMemberJoined, domain.TargetInvitation, "wrong_invitee", now,
		).InProject(found.ProjectID).On(found.ID))
		return Joined{}, domain.ErrWrongInvitee
	}

	entry := domain.Allow(
		found.OrgID, nil, domain.ActionMemberJoined, domain.TargetMembership, now,
	).InProject(found.ProjectID).With("role", string(found.Role))

	joined, err := s.invitations.Accept(ctx, Acceptance{
		Invitation: found.Invitation,
		Invitee:    profile,
		NewUserID:  uuid.New(),
		At:         now,
		Entry:      entry,
	})
	if err != nil {
		return Joined{}, err
	}

	s.log.InfoContext(ctx, "invitation accepted",
		"org_id", found.OrgID, "project_id", found.ProjectID, "invitation_id", found.ID, "user_id", joined.UserID)
	return Joined{Identity: joined, ProjectID: found.ProjectID}, nil
}

// HashToken is how a token is kept: the hex of its SHA-256. A token is 256 bits
// of randomness, so no salt or slow hash is needed to make guessing the
// original from the hash impractical.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *Service) require(ctx context.Context, who domain.Identity, projectID uuid.UUID) error {
	allowed, err := s.authz.Can(ctx, who, domain.PermManageMembers, projectID)
	if err != nil {
		return err
	}
	if !allowed {
		s.log.InfoContext(ctx, "request refused",
			"org_id", who.OrgID, "user_id", who.UserID, "project_id", projectID)
		return domain.ErrForbidden
	}
	return nil
}

func (s *Service) refused(ctx context.Context, entry domain.AuditEntry) {
	if err := s.journal.Record(ctx, entry); err != nil {
		s.log.ErrorContext(ctx, "record a refusal", "action", entry.Action, "error", err)
	}
}

package domain

import (
	"time"

	"github.com/google/uuid"
)

// AuditAction is one kind of thing worth remembering.
//
// The values are stored in audit_log.action and are therefore part of the data,
// not an implementation detail: renaming one rewrites nothing, it simply makes
// every entry already written unreadable by whoever looks for the new name.
type AuditAction string

const (
	// ActionRunQueued is a run accepted and waiting for a worker.
	ActionRunQueued AuditAction = "run.queued"
	// ActionRunRefused is a run that was not accepted. The reason is in the
	// entry's details, and the outcome says it was a refusal.
	ActionRunRefused AuditAction = "run.refused"
	// ActionRunCancelled is somebody stopping one run.
	ActionRunCancelled AuditAction = "run.cancelled"
	// ActionRunsStopped is the emergency stop: every run of the organization
	// at once. It belongs to no project.
	ActionRunsStopped AuditAction = "runs.stopped"
	// ActionTargetVerified is a target that served its challenge, or failed to.
	ActionTargetVerified AuditAction = "target.verified"
	// ActionTargetApproved is the human decision that lets load reach a host.
	// The one entry an abuse report is answered with.
	ActionTargetApproved AuditAction = "target.approved"
	// ActionRoleChanged is a membership granted or changed.
	ActionRoleChanged AuditAction = "member.role_changed"
	// ActionMemberRemoved is a membership revoked.
	ActionMemberRemoved AuditAction = "member.removed"
)

// AuditOutcome says whether what was attempted was allowed.
type AuditOutcome string

const (
	// Allowed means the action went through.
	Allowed AuditOutcome = "allowed"
	// Refused means it did not. A refusal is as worth recording as a success:
	// an attempt to load an unverified target is exactly what this is for.
	Refused AuditOutcome = "refused"
)

// Target types, stored in audit_log.target_type. Spelled out rather than
// derived from a Go type name, so that renaming a struct does not change what
// past entries mean.
const (
	TargetRun         = "run"
	TargetEnvironment = "environment"
	TargetMembership  = "membership"
	TargetOrg         = "organization"
)

// AuditEntry is one line of the journal: who did what, to which target, with
// what result, and when.
//
// What it must never carry is as important as what it carries. Details holds a
// closed set of keys chosen per action — never free text taken from a request,
// never a secret, a token, a verification challenge or a header. A journal that
// becomes a second place credentials end up is worse than no journal, because
// it is read by people who are trusted to read everything.
type AuditEntry struct {
	OrgID uuid.UUID
	// ActorID is the person responsible, and nil when Ballast itself is: a
	// worker refusing to start a run that was accepted hours ago acted on
	// nobody's behalf.
	ActorID *uuid.UUID
	// ProjectID is the project concerned, and nil for an action on the whole
	// organization. Those are read from every project's journal.
	ProjectID *uuid.UUID
	Action    AuditAction
	Outcome   AuditOutcome
	// TargetType is one of the constants above; TargetID is nil when the
	// action has no single target, as an emergency stop has none.
	TargetType string
	TargetID   *uuid.UUID
	Details    map[string]string
	At         time.Time
}

// Allow records an action that went through.
func Allow(
	orgID uuid.UUID, actor *uuid.UUID, action AuditAction, targetType string, at time.Time,
) AuditEntry {
	return AuditEntry{
		OrgID:      orgID,
		ActorID:    actor,
		Action:     action,
		Outcome:    Allowed,
		TargetType: targetType,
		At:         at,
	}
}

// Refuse records an attempt that was not allowed, with the reason it was not.
//
// The reason is a value this code chose, never a message that travelled in from
// outside: an error from deeper down can name a table, a column, or an
// identifier belonging to somebody else.
func Refuse(
	orgID uuid.UUID, actor *uuid.UUID, action AuditAction, targetType, reason string, at time.Time,
) AuditEntry {
	return AuditEntry{
		OrgID:      orgID,
		ActorID:    actor,
		Action:     action,
		Outcome:    Refused,
		TargetType: targetType,
		Details:    map[string]string{"reason": reason},
		At:         at,
	}
}

// InProject says which project the action concerned.
func (e AuditEntry) InProject(projectID uuid.UUID) AuditEntry {
	e.ProjectID = &projectID
	return e
}

// On says which row the action was about.
func (e AuditEntry) On(targetID uuid.UUID) AuditEntry {
	e.TargetID = &targetID
	return e
}

// With adds one detail. Keys are chosen by the code that records the action and
// are part of what the journal promises; values are never user input that could
// carry a credential.
func (e AuditEntry) With(key, value string) AuditEntry {
	details := make(map[string]string, len(e.Details)+1)
	for k, v := range e.Details {
		details[k] = v
	}
	details[key] = value

	e.Details = details
	return e
}

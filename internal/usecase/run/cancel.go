package run

import (
	"context"
	"fmt"
	"strconv"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// cancelledMessage is what a cancelled run says about itself afterwards. It is
// stored on the run, so that months later its history reads as a decision
// somebody took rather than as an unexplained ending.
const cancelledMessage = "the run was cancelled"

// stoppedMessage is the same for a run caught by the emergency stop. The two
// are told apart on purpose: one run stopped is somebody changing their mind,
// and every run stopped at once is an incident.
const stoppedMessage = "the run was cancelled by an emergency stop"

// Cancel stops one run, and reports whether it is over or merely asked to stop.
//
// A queued run is over when this returns: nothing had started, so nothing had
// to be stopped before saying so. A run being executed is only asked — its
// generator is loading somebody's environment, and the status that says the
// load has stopped is the worker's to write, once it has destroyed the
// container and freed the target. The worker sees the request within a couple
// of seconds.
//
// A run of a project the caller cannot see answers as a run that is not there,
// for the reason ByID does: a refusal would confirm that the identifier stands
// for something.
func (s *Service) Cancel(
	ctx context.Context, who domain.Identity, id uuid.UUID,
) (domain.CancelOutcome, error) {
	found, err := s.runs.ByID(ctx, who.OrgID, id)
	if err != nil {
		return "", err
	}

	allowed, err := s.authz.Can(ctx, who, domain.PermCancelRun, found.ProjectID)
	if err != nil {
		return "", err
	}
	if !allowed {
		s.log.InfoContext(ctx, "cancellation refused for want of permission",
			"org_id", who.OrgID, "user_id", who.UserID,
			"project_id", found.ProjectID, "run_id", id)
		s.record(ctx, domain.Refuse(
			who.OrgID, &who.UserID, domain.ActionRunCancelled, domain.TargetRun,
			"the caller may not cancel runs in this project", s.clock.Now(),
		).InProject(found.ProjectID).On(id))
		return "", domain.ErrNotFound
	}

	// Asked of the status read above rather than left to the statement alone,
	// so that the refusal comes from the state machine. The statement checks it
	// again, which is what settles the race: between this read and the write,
	// a worker may have started or finished the run.
	if found.Status.IsTerminal() {
		return "", domain.ErrNotCancellable
	}

	outcome, err := s.runs.Cancel(ctx, who.OrgID, id, s.clock.Now(), cancelledMessage)
	if err != nil {
		return "", err
	}

	// The outcome is in the entry because the two are different facts: a queued
	// run is over when this returns, a running one has been asked to stop and
	// is ended by the worker that stops it.
	s.record(ctx, domain.Allow(
		who.OrgID, &who.UserID, domain.ActionRunCancelled, domain.TargetRun, s.clock.Now(),
	).InProject(found.ProjectID).On(id).With("result", string(outcome)))

	s.log.InfoContext(ctx, "run cancelled",
		"run_id", id, "project_id", found.ProjectID,
		"user_id", who.UserID, "outcome", outcome)

	return outcome, nil
}

// StopEverything is the emergency stop: nothing else starts, and everything
// under way is asked to stop.
//
// It is an administrator's to use, and an administrator of the whole
// organization: the question is asked once per project, which is what keeps
// every check scoped — a permission says what somebody may do, never what to.
// Somebody who administers some of the projects is refused rather than given a
// stop that silently leaves the rest running; cancelling run by run is still
// open to them.
//
// Running runs are asked, not ended, for the reason Cancel gives: each worker
// takes its own container down and writes its own run's ending.
func (s *Service) StopEverything(ctx context.Context, who domain.Identity) (int, int, error) {
	projects, err := s.projects.ProjectIDs(ctx, who.OrgID)
	if err != nil {
		return 0, 0, fmt.Errorf("list the projects: %w", err)
	}

	for _, project := range projects {
		// Two permissions, and both are asked. PermManageProject is held by no
		// mask but an administrator's, which is what "reserved to admin" means
		// here; PermCancelRun is what the action actually is. An exception mask
		// written by hand could grant either without the other.
		for _, needed := range []domain.Permission{domain.PermManageProject, domain.PermCancelRun} {
			allowed, err := s.authz.Can(ctx, who, needed, project)
			if err != nil {
				return 0, 0, err
			}
			if !allowed {
				s.log.WarnContext(ctx, "emergency stop refused: the caller does not administer every project",
					"org_id", who.OrgID, "user_id", who.UserID, "project_id", project)
				// On the organization, like the stop itself: somebody tried to
				// stop everything and was not allowed to, which every project
				// has an interest in knowing.
				s.record(ctx, domain.Refuse(
					who.OrgID, &who.UserID, domain.ActionRunsStopped, domain.TargetOrg,
					"the caller does not administer every project of the organization",
					s.clock.Now(),
				))
				return 0, 0, domain.ErrForbidden
			}
		}
	}

	cancelled, asked, err := s.runs.CancelEverything(ctx, who.OrgID, s.clock.Now(), stoppedMessage)
	if err != nil {
		return 0, 0, err
	}

	// No project: the action is on the organization, and it is read from every
	// project's journal rather than written once per project.
	s.record(ctx, domain.Allow(
		who.OrgID, &who.UserID, domain.ActionRunsStopped, domain.TargetOrg, s.clock.Now(),
	).With("cancelled", strconv.Itoa(cancelled)).
		With("asked_to_stop", strconv.Itoa(asked)))

	// At error level deliberately: an emergency stop is not routine, and the
	// line that records it is the one somebody will look for afterwards.
	s.log.ErrorContext(ctx, "emergency stop",
		"org_id", who.OrgID, "user_id", who.UserID,
		"cancelled", cancelled, "asked_to_stop", asked)

	return cancelled, asked, nil
}

// requireAdministratorEverywhere refuses anybody who does not administer every
// project of the organization.
//
// Project by project, because a permission says what somebody may do and never
// what to: there is no organization-wide question to ask. Used by the readings
// and the actions that are about the whole organization rather than one of its
// projects.
func (s *Service) requireAdministratorEverywhere(ctx context.Context, who domain.Identity) error {
	projects, err := s.projects.ProjectIDs(ctx, who.OrgID)
	if err != nil {
		return fmt.Errorf("list the projects: %w", err)
	}

	for _, project := range projects {
		allowed, err := s.authz.Can(ctx, who, domain.PermManageProject, project)
		if err != nil {
			return err
		}
		if !allowed {
			s.log.InfoContext(ctx, "an organization-wide reading was refused",
				"org_id", who.OrgID, "user_id", who.UserID, "project_id", project)
			return domain.ErrForbidden
		}
	}

	return nil
}

package run

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

// TooManyRuns is a refusal by a limit rather than by a right: the same request
// later is fine, and the caller is told how much later.
type TooManyRuns struct {
	// Reason is this code's own words, stored in the journal and shown to the
	// caller. It says which limit, never how close anybody else is to it.
	Reason string
	// RetryAfter is how long until the limit would let this through, as far as
	// it can be known. Zero when nothing can be promised — a queue empties
	// when runs finish, and nobody knows when that is.
	RetryAfter time.Duration
}

func (e TooManyRuns) Error() string { return e.Reason }

// Is makes this answer to domain.ErrTooManyRuns, so callers match on the
// sentinel and the handler needs to know nothing about this type to map it.
func (e TooManyRuns) Is(target error) bool { return target == domain.ErrTooManyRuns }

// withinLimits refuses a run that would go past a limit nobody can raise.
//
// Asked before anything is read about the scenario or the target, and before
// anything is written: a caller past their limit is past it whatever they were
// going to ask for, and reading the rest would be work done for a refusal.
//
// Counted from the runs already in the database rather than from a counter in
// this process. Two reasons, and the second is the one that matters: a counter
// in memory is wrong the moment a second API process exists, and it forgets
// everything when one restarts — which is exactly when somebody abusing the
// thing would like it to.
//
// What this does not stop: somebody hammering the endpoint with requests that
// are refused for other reasons. Those create no run, so they never reach this
// count. Keeping a flood of refusals out is a different job, done in front of
// the application rather than inside it.
func (s *Service) withinLimits(ctx context.Context, who domain.Identity) error {
	now := s.clock.Now()

	queued, err := s.runs.Queued(ctx, who.OrgID)
	if err != nil {
		return fmt.Errorf("count the runs waiting: %w", err)
	}
	if queued >= domain.MaxQueuedRunsPerOrg {
		return s.refuseByLimit(ctx, who, TooManyRuns{
			Reason: fmt.Sprintf(
				"this organization already has %d runs waiting, which is the most it may queue",
				queued),
		}, "queued", strconv.Itoa(queued))
	}

	since := now.Add(-domain.RateWindow)

	triggered, err := s.triggeredSince(ctx, who, since)
	if err != nil {
		return err
	}

	// A person is counted by what they have triggered, and a pipeline by what its
	// token has: a token has no person, and a person's count is neither what holds
	// a token nor what spares one. Both are held to the same figure.
	if who.IsWebhook() {
		if triggered.ByWebhook >= domain.MaxRunsPerUserPerWindow {
			return s.refuseByLimit(ctx, who, TooManyRuns{
				Reason: fmt.Sprintf("this webhook has triggered %d runs in the last %s, which is the most allowed",
					triggered.ByWebhook, domain.RateWindow),
				RetryAfter: s.untilWebhook(ctx, who.OrgID, who.WebhookID, since, now),
			}, "triggered_by_webhook", strconv.Itoa(triggered.ByWebhook))
		}
	} else if who.IsToken() {
		if triggered.ByToken >= domain.MaxRunsPerUserPerWindow {
			return s.refuseByLimit(ctx, who, TooManyRuns{
				Reason: fmt.Sprintf("this token has triggered %d runs in the last %s, which is the most allowed",
					triggered.ByToken, domain.RateWindow),
				RetryAfter: s.untilToken(ctx, who.OrgID, who.TokenID, since, now),
			}, "triggered_by_token", strconv.Itoa(triggered.ByToken))
		}
	} else if triggered.ByUser >= domain.MaxRunsPerUserPerWindow {
		return s.refuseByLimit(ctx, who, TooManyRuns{
			Reason: fmt.Sprintf("you have triggered %d runs in the last %s, which is the most allowed",
				triggered.ByUser, domain.RateWindow),
			RetryAfter: s.until(ctx, who.OrgID, who.UserID, since, now),
		}, "triggered_by_user", strconv.Itoa(triggered.ByUser))
	}

	// The organization's own total counts whoever launched the runs: a token is one
	// way into the organization, and not a way round its limit.
	if triggered.ByOrg >= domain.MaxRunsPerOrgPerWindow {
		return s.refuseByLimit(ctx, who, TooManyRuns{
			Reason: fmt.Sprintf(
				"this organization has triggered %d runs in the last %s, which is the most allowed",
				triggered.ByOrg, domain.RateWindow),
			RetryAfter: s.until(ctx, who.OrgID, uuid.Nil, since, now),
		}, "triggered_by_org", strconv.Itoa(triggered.ByOrg))
	}

	return nil
}

// triggeredSince is the count the caller's limit is read from: the organization's
// and that of whoever is asking.
func (s *Service) triggeredSince(ctx context.Context, who domain.Identity, since time.Time) (Triggered, error) {
	if who.IsWebhook() {
		triggered, err := s.runs.TriggeredByWebhookSince(ctx, who.OrgID, who.WebhookID, since)
		if err != nil {
			return Triggered{}, fmt.Errorf("count the runs triggered: %w", err)
		}
		return triggered, nil
	}
	if who.IsToken() {
		triggered, err := s.runs.TriggeredByTokenSince(ctx, who.OrgID, who.TokenID, since)
		if err != nil {
			return Triggered{}, fmt.Errorf("count the runs triggered: %w", err)
		}
		return triggered, nil
	}

	triggered, err := s.runs.TriggeredSince(ctx, who.OrgID, who.UserID, since)
	if err != nil {
		return Triggered{}, fmt.Errorf("count the runs triggered: %w", err)
	}
	return triggered, nil
}

// until is how long before the oldest run of the window leaves it, which is
// when the next slot opens.
//
// Best effort: a figure that cannot be read is no figure at all rather than a
// guess. Telling a caller to come back in a minute when the truth is an hour
// is worse than telling them nothing.
func (s *Service) until(
	ctx context.Context, orgID, userID uuid.UUID, since, now time.Time,
) time.Duration {
	oldest, found, err := s.runs.OldestSince(ctx, orgID, userID, since)
	if err != nil || !found {
		return 0
	}

	// The oldest run leaves the window one window after it was created.
	wait := oldest.Add(domain.RateWindow).Sub(now)
	if wait < 0 {
		return 0
	}

	return wait
}

// untilToken is until for a token's own window: how long before the oldest run
// the token launched leaves it.
func (s *Service) untilToken(
	ctx context.Context, orgID, tokenID uuid.UUID, since, now time.Time,
) time.Duration {
	oldest, found, err := s.runs.OldestByTokenSince(ctx, orgID, tokenID, since)
	if err != nil || !found {
		return 0
	}

	wait := oldest.Add(domain.RateWindow).Sub(now)
	if wait < 0 {
		return 0
	}

	return wait
}

// untilWebhook is until for a webhook's own window.
func (s *Service) untilWebhook(
	ctx context.Context, orgID, webhookID uuid.UUID, since, now time.Time,
) time.Duration {
	oldest, found, err := s.runs.OldestByWebhookSince(ctx, orgID, webhookID, since)
	if err != nil || !found {
		return 0
	}

	wait := oldest.Add(domain.RateWindow).Sub(now)
	if wait < 0 {
		return 0
	}

	return wait
}

// refuseByLimit records the refusal and returns it.
//
// In the journal as well as the log, because a limit reached is the shape of
// abuse: one refusal is somebody busy, a page of them is somebody looping.
func (s *Service) refuseByLimit(
	ctx context.Context, who domain.Identity, refusal TooManyRuns, limit, reached string,
) error {
	s.log.WarnContext(ctx, "run refused by a limit",
		"org_id", who.OrgID, "user_id", who.UserID, "token_id", who.TokenID, "webhook_id", who.WebhookID,
		"limit", limit, "reached", reached)

	s.record(ctx, refused(who, refusal.Reason, s.clock.Now()).
		With("limit", limit).
		With("reached", reached))

	return refusal
}

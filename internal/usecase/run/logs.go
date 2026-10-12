package run

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/youssefmahersi/ballast/internal/domain"
)

const (
	// logFlushInterval is how long a line may wait to be stored. Half a second
	// is what makes "live" true: the viewer polls once a second, so a line is
	// on the screen about a second and a half after the generator wrote it.
	logFlushInterval = 500 * time.Millisecond
	// logBatchSize sends a batch early when a run is chatty. A k6 script in a
	// loop can write thousands of lines a second, and waiting out the interval
	// would mean holding them all.
	logBatchSize = 200
	// logBuffer is how far the generator may run ahead of the storing. Beyond
	// it the runner's own writer blocks, which slows the container rather than
	// losing its output.
	logBuffer = 512

	// MaxLogPage is the most lines one read may return. A finished run can hold
	// a hundred thousand; the stream asks again while a page comes back full.
	//
	// Exported because a caller streaming the lines has to know what a full
	// page is: that is how it tells "there is more waiting" from "that is all
	// for now", and whether to ask again at once or in a second.
	MaxLogPage = 500
)

// LogsSince returns the lines of a run a viewer has not seen, along with the
// run's status.
//
// The status comes back with them because it is what says whether to keep
// waiting: a terminal status and an empty page together mean there will never
// be another line.
//
// The permission is resolved on every call, which is the rule for every request
// and is not relaxed for a stream that stays open: somebody removed from a
// project stops reading its output within a second, with no exception to
// explain. It costs two queries a second per viewer, and a run has few viewers.
//
// A run of a project the caller cannot see answers as a run that is not there,
// for the reason ByID does.
func (s *Service) LogsSince(
	ctx context.Context, who domain.Identity, runID uuid.UUID, after int64, limit int,
) ([]domain.LogLine, domain.RunStatus, error) {
	found, err := s.runs.ByID(ctx, who.OrgID, runID)
	if err != nil {
		return nil, "", err
	}

	allowed, err := s.authz.Can(ctx, who, domain.PermViewProject, found.ProjectID)
	if err != nil {
		return nil, "", err
	}
	if !allowed {
		s.log.InfoContext(ctx, "run logs hidden for want of permission",
			"org_id", who.OrgID, "user_id", who.UserID,
			"project_id", found.ProjectID, "run_id", runID)
		return nil, "", domain.ErrNotFound
	}

	if limit <= 0 || limit > MaxLogPage {
		limit = MaxLogPage
	}

	lines, err := s.logs.Since(ctx, who.OrgID, runID, after, limit)
	if err != nil {
		return nil, "", err
	}

	return lines, found.Status, nil
}

// capture stores what the generator writes, and passes it on.
//
// It returns the channel to hand the executor and a channel closed once
// everything written has been dealt with. The caller closes the first when the
// execution is over and waits on the second, which is what guarantees that the
// last line a run produced is stored before the run is recorded as finished.
//
// Every line is redacted once, here, and the redacted line is what goes both to
// the database and onward to the caller. Redacting only on the way to storage
// would mean "no credential in the logs" was true of the table and false of the
// worker's own output.
func (s *Service) capture(
	ctx context.Context,
	orgID, runID uuid.UUID,
	redactor domain.Redactor,
	out chan<- domain.LogLine,
) (chan domain.LogLine, <-chan struct{}) {
	in := make(chan domain.LogLine, logBuffer)
	done := make(chan struct{})

	go func() {
		defer close(done)

		ticker := time.NewTicker(s.flushEvery)
		defer ticker.Stop()

		batch := make([]domain.LogLine, 0, logBatchSize)

		flush := func() {
			if len(batch) == 0 {
				return
			}

			// Without the execution's cancellation: the last lines of a
			// cancelled or timed-out run are the ones that say what happened,
			// so they are exactly the lines that must still be written.
			writing, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()

			if err := s.logs.Append(writing, orgID, runID, batch); err != nil {
				// Logged and dropped. A line is not a measurement: losing some
				// output is worth less than wedging a run that is generating
				// load, and the next batch is a fresh attempt.
				s.log.WarnContext(ctx, "a batch of log lines could not be stored",
					"run_id", runID, "lines", len(batch), "error", err)
			}

			// The repository copies what it is given before it writes, so the
			// backing array can be reused.
			batch = batch[:0]
		}

		for {
			select {
			case line, open := <-in:
				if !open {
					flush()
					return
				}

				line.Message = redactor.Redact(line.Message)

				if out != nil {
					// A caller that stopped reading must not wedge the run.
					select {
					case out <- line:
					case <-ctx.Done():
					}
				}

				batch = append(batch, line)
				if len(batch) >= logBatchSize {
					flush()
				}

			case <-ticker.C:
				flush()
			}
		}
	}()

	return in, done
}

// secretsOf is what of a job's environment is a credential: everything except
// the variables Ballast writes itself.
//
// BASE_URL is excluded deliberately. It is not a secret, and masking it would
// replace the target's address in every line a generator prints — destroying
// the one thing the screen is there to show.
func secretsOf(job Job) map[string]string {
	secrets := make(map[string]string, len(job.Env))
	for key, value := range job.Env {
		if key == domain.BaseURLVariable {
			continue
		}
		secrets[key] = value
	}
	return secrets
}

// redactorFor builds the censor for one run's output, and complains about what
// it cannot censor.
func (s *Service) redactorFor(ctx context.Context, runID uuid.UUID, job Job) domain.Redactor {
	redactor, tooShort := domain.NewRedactor(secretsOf(job))

	if len(tooShort) > 0 {
		// By key. A warning that printed the value would be the leak the
		// redactor exists to prevent.
		s.log.WarnContext(ctx, "some secrets are too short to be kept out of this run's logs",
			"run_id", runID, "keys", fmt.Sprint(tooShort))
	}

	return redactor
}

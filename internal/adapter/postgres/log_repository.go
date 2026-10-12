package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/youssefmahersi/ballast/internal/adapter/postgres/sqlc"
	"github.com/youssefmahersi/ballast/internal/domain"
)

// LogRepository stores the lines a run's generator wrote, and hands them back
// in order.
//
// Separate from RunRepository because the two are used by different processes
// for different reasons: the worker writes here while a run is under way, and
// the API reads here, once a second, for as long as somebody is watching.
type LogRepository struct {
	db *DB
}

// NewLogRepository returns a repository over db.
func NewLogRepository(db *DB) *LogRepository {
	return &LogRepository{db: db}
}

// Append stores a batch of lines.
//
// The sequence numbers come from the runner, which shares one counter between
// the two streams, so the order they are stored in is the order the generator
// actually produced them — stdout and stderr interleaved as they happened.
//
// A batch that was already written is not written again: the statement leaves a
// conflicting sequence number alone. A line stored twice is a line the live
// view shows twice, and a worker whose acknowledgement was lost has no way of
// knowing which it was.
func (r *LogRepository) Append(
	ctx context.Context, orgID, runID uuid.UUID, lines []domain.LogLine,
) error {
	if len(lines) == 0 {
		return nil
	}

	seqs := make([]int64, 0, len(lines))
	streams := make([]string, 0, len(lines))
	messages := make([]string, 0, len(lines))
	at := make([]time.Time, 0, len(lines))
	for _, line := range lines {
		seqs = append(seqs, line.Seq)
		streams = append(streams, line.Stream)
		messages = append(messages, line.Message)
		at = append(at, line.At)
	}

	return r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		if err := sqlc.New(tx).AppendRunLogs(ctx, sqlc.AppendRunLogsParams{
			OrgID:   orgID,
			RunID:   runID,
			Column3: seqs,
			Column4: streams,
			Column5: messages,
			Column6: at,
		}); err != nil {
			return fmt.Errorf("append the run's logs: %w", err)
		}
		return nil
	})
}

// Since returns at most limit lines following after, in order.
//
// after is exclusive, which is what makes a reconnection exact: a client says
// the sequence number of the last line it holds and gets what follows, with no
// line sent twice and none skipped.
func (r *LogRepository) Since(
	ctx context.Context, orgID, runID uuid.UUID, after int64, limit int,
) ([]domain.LogLine, error) {
	var lines []domain.LogLine

	err := r.db.InTenantTx(ctx, orgID, func(tx pgx.Tx) error {
		rows, err := sqlc.New(tx).RunLogsSince(ctx, sqlc.RunLogsSinceParams{
			OrgID: orgID,
			RunID: runID,
			Seq:   after,
			// The ceiling is the caller's, and it is small: a page is read, sent
			// and asked for again.
			Limit: int32(limit), //nolint:gosec // bounded by the caller
		})
		if err != nil {
			return fmt.Errorf("read the run's logs: %w", err)
		}

		lines = make([]domain.LogLine, 0, len(rows))
		for _, row := range rows {
			lines = append(lines, domain.LogLine{
				Seq:     row.Seq,
				Stream:  row.Stream,
				Message: row.Message,
				At:      row.LoggedAt,
			})
		}
		return nil
	})

	return lines, err
}

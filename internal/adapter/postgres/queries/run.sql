-- Every statement here runs inside InTenantTx, so row-level security already
-- restricts it to one organization. org_id is still named in each WHERE: it is
-- the first column of every index, and a policy is a safety net rather than a
-- query plan.

-- name: CreateRun :one
INSERT INTO run (
    org_id, project_id, scenario_id, environment_id,
    scenario_version, profile, profile_params, triggered_by, status
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'queued')
RETURNING *;

-- name: GetRun :one
SELECT * FROM run WHERE org_id = $1 AND id = $2;

-- Newest first, served by run_org_project_created_idx. id breaks ties, so two
-- runs created in the same instant always come back in the same order, and a
-- page boundary between them loses or repeats neither. With a cursor, only the
-- runs strictly older than it: (created_at, id) compared as a pair, which is
-- the order the list is in.
-- name: ListProjectRuns :many
SELECT * FROM run
WHERE org_id = @org_id AND project_id = @project_id
  AND (
    sqlc.narg(before_created_at)::timestamptz IS NULL
    OR (created_at, id) < (sqlc.narg(before_created_at)::timestamptz, sqlc.narg(before_id)::uuid)
  )
ORDER BY created_at DESC, id DESC
LIMIT @row_limit;

-- The status in the WHERE clause is the guard: a run somebody else already
-- finished is not finished twice.
-- name: FinishRun :one
UPDATE run
SET status = $3, ended_at = $4, error_message = $5
WHERE org_id = $1 AND id = $2 AND status = 'running'
RETURNING *;

-- name: GetScenario :one
SELECT * FROM scenario WHERE org_id = $1 AND id = $2;

-- name: GetEnvironment :one
SELECT * FROM environment WHERE org_id = $1 AND id = $2;

-- name: ListScenarioThresholds :many
SELECT * FROM threshold
WHERE org_id = $1 AND scenario_id = $2
ORDER BY metric, expression;

-- One round trip rather than one per measured number: a single run reports a
-- hundred or so.
-- name: InsertRunMetrics :exec
INSERT INTO run_metric (org_id, run_id, name, tag_name, stat, value)
SELECT $1, $2, unnest($3::text[]), unnest($4::text[]), unnest($5::text[]), unnest($6::float8[]);

-- name: InsertRunThresholds :exec
INSERT INTO run_threshold (org_id, run_id, metric, expression, passed)
SELECT $1, $2, unnest($3::text[]), unnest($4::text[]), unnest($5::bool[]);

-- name: ListRunMetrics :many
SELECT * FROM run_metric
WHERE org_id = $1 AND run_id = $2
ORDER BY name, tag_name, stat;

-- name: ListRunThresholds :many
SELECT * FROM run_threshold
WHERE org_id = $1 AND run_id = $2
ORDER BY metric, expression;

-- The next run that could start: queued, and on an environment nothing is
-- holding.
--
-- FOR UPDATE SKIP LOCKED keeps two workers off the same run. The NOT EXISTS is
-- not what keeps two runs off the same environment — the lock's primary key
-- does that — it is what lets a worker walk past a busy environment's queue and
-- pick up a run on a free one instead. Without it a worker would sit on the
-- oldest run until its target was free, and a second environment would wait for
-- the first.
-- name: NextClaimableRun :one
SELECT r.* FROM run AS r
WHERE r.org_id = $1 AND r.status = 'queued'
  AND NOT EXISTS (
    SELECT 1 FROM environment_lock AS held
    WHERE held.org_id = r.org_id
      AND held.environment_id = r.environment_id
      AND held.expires_at > $2
  )
ORDER BY r.created_at
LIMIT 1
FOR UPDATE SKIP LOCKED;

-- Take the environment, or find out somebody else has it.
--
-- One statement for both cases. It inserts when nothing holds the environment,
-- and takes over when what holds it has expired. A live lock matches the WHERE
-- on neither count, so the statement returns no row — which is the caller's
-- signal to leave its run queued.
--
-- Two concurrent attempts do not race: the second blocks on the first's
-- uncommitted row, and once that commits it re-evaluates the WHERE and comes
-- back empty rather than raising a unique violation. Checked against a real
-- database, because the whole design rests on it.
-- name: AcquireEnvironmentLock :one
INSERT INTO environment_lock (org_id, environment_id, run_id, acquired_at, expires_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (org_id, environment_id) DO UPDATE
SET run_id = excluded.run_id,
    acquired_at = excluded.acquired_at,
    expires_at = excluded.expires_at
WHERE environment_lock.expires_at <= $4
RETURNING *;

-- The status is in the WHERE rather than assumed: between the select above and
-- this, nothing else may have started the same run.
-- name: StartRun :one
UPDATE run
SET status = 'running', started_at = $3, heartbeat_at = $3
WHERE org_id = $1 AND id = $2 AND status = 'queued'
RETURNING *;

-- A release names its run, so that a lock taken over after an expiry is not
-- dropped by the worker whose run lost it.
-- name: ReleaseEnvironmentLock :execrows
DELETE FROM environment_lock
WHERE org_id = $1 AND environment_id = $2 AND run_id = $3;

-- The run says it is still being watched.
--
-- The status is in the WHERE and the row count is returned, which is how a
-- worker finds out it no longer owns what it is executing: a collector that
-- declared this run abandoned moved it out of running, so this touches nothing.
-- name: TouchRun :execrows
UPDATE run
SET heartbeat_at = $3
WHERE org_id = $1 AND id = $2 AND status = 'running';

-- Runs whose worker has stopped saying anything.
--
-- orphaned rather than errored, because orphaned does not mean finished: it
-- means nobody is watching and the container may well still be hammering the
-- target. What turns it into errored is the collector, once it has destroyed
-- what was left.
--
-- A null heartbeat counts as stale. StartRun always writes one, so a running run
-- without one was started by something that did not — which is not a reason to
-- leave it alone for ever.
--
-- Two collectors do not both take a run: the second blocks on the first's
-- uncommitted row, then re-evaluates the WHERE against the committed version,
-- sees a status that is no longer running, and skips it.
-- name: OrphanStaleRuns :many
UPDATE run
SET status = 'orphaned'
WHERE org_id = $1 AND status = 'running'
  AND (heartbeat_at IS NULL OR heartbeat_at <= $2)
RETURNING *;

-- Every run waiting to be cleaned up, including one a collector that died
-- itself left half-handled.
-- name: ListOrphanedRuns :many
SELECT * FROM run
WHERE org_id = $1 AND status = 'orphaned'
ORDER BY created_at;

-- The end of an abandoned run, written only after its container is gone.
--
-- The status in the WHERE is what makes this safe to call twice: the second
-- caller touches no row rather than overwriting the first's ending.
-- name: FailOrphanedRun :one
UPDATE run
SET status = 'errored', ended_at = $3, error_message = $4
WHERE org_id = $1 AND id = $2 AND status = 'orphaned'
RETURNING *;

-- The runs a container is allowed to exist for: being executed, and watched.
--
-- Read when a worker starts, to decide what on the machine is rubbish. A
-- running run whose heartbeat has gone stale is not in the list: it is about to
-- be collected anyway, and its container has nobody watching it either.
-- name: LiveRunIDs :many
SELECT id FROM run
WHERE org_id = $1 AND status = 'running' AND heartbeat_at > $2;

-- Ask a run being executed to stop.
--
-- coalesce keeps the first request's instant: asking twice is one decision, and
-- the second asker must not move the moment it was taken. No row means the run
-- is not being executed — finished, or still queued, which the caller handles
-- with the statement below.
-- name: RequestRunCancellation :one
UPDATE run
SET cancel_requested_at = coalesce(cancel_requested_at, $3)
WHERE org_id = $1 AND id = $2 AND status = 'running'
RETURNING *;

-- Cancel a run nothing has started.
--
-- Immediate, and safe to be: a queued run holds no environment lock and has no
-- container, so there is nothing whose stopping has to be waited for.
-- name: CancelQueuedRun :one
UPDATE run
SET status = 'cancelled', ended_at = $3, error_message = $4, cancel_requested_at = $3
WHERE org_id = $1 AND id = $2 AND status = 'queued'
RETURNING *;

-- The emergency stop, first half: nothing else starts.
-- name: CancelEveryQueuedRun :many
UPDATE run
SET status = 'cancelled', ended_at = $2, error_message = $3, cancel_requested_at = $2
WHERE org_id = $1 AND status = 'queued'
RETURNING *;

-- The emergency stop, second half: everything under way is asked to stop. Each
-- worker sees its own run's request and takes its own container down, which is
-- the only place that can be done.
-- name: RequestEveryRunningCancellation :many
UPDATE run
SET cancel_requested_at = coalesce(cancel_requested_at, $2)
WHERE org_id = $1 AND status = 'running'
RETURNING *;

-- The lines a generator wrote, as the worker collects them.
--
-- One round trip per batch rather than per line: a chatty run writes thousands,
-- and the live view wants them promptly, not individually.
--
-- DO NOTHING rather than an error, on the unique (org_id, run_id, seq): a batch
-- that was written and whose acknowledgement was lost is written again by a
-- worker that has no way of knowing, and a line stored twice would be a line
-- the live view shows twice.
-- name: AppendRunLogs :exec
INSERT INTO run_log (org_id, run_id, seq, stream, message, logged_at)
SELECT $1, $2,
       unnest($3::bigint[]), unnest($4::text[]), unnest($5::text[]), unnest($6::timestamptz[])
ON CONFLICT (org_id, run_id, seq) DO NOTHING;

-- What a viewer has not seen yet.
--
-- seq > $3 is the whole of the resumption: the sequence number is what the
-- stream sends as each event's id, so a client that reconnects says the last
-- one it holds and gets what follows, once.
--
-- Limited, because the first read of a finished run would otherwise be every
-- line it ever wrote. The caller asks again while a page comes back full.
-- name: RunLogsSince :many
SELECT * FROM run_log
WHERE org_id = $1 AND run_id = $2 AND seq > $3
ORDER BY seq
LIMIT $4;

-- What the run moved, written beside the metrics it came from.
--
-- Two measurements: what the proxy counted leaving the machine, and what the
-- generator reported. Written in the transaction that writes the metrics, from
-- the same parsed summary, so the copy on the run and the rows in run_metric
-- cannot drift apart.
-- name: RecordRunTraffic :exec
UPDATE run
SET egress_bytes        = $3,
    ingress_bytes       = $4,
    data_sent_bytes     = $5,
    data_received_bytes = $6,
    traffic_measured    = $7
WHERE org_id = $1 AND id = $2;

-- One organization's traffic over a period, from the runs that ended inside it.
--
-- The lower bound is included and the upper is not: two consecutive months must
-- not both count a run that ended at midnight. A run with no end has no volumes
-- either, and the comparison leaves it out on its own.
-- name: TrafficInPeriod :one
SELECT
    coalesce(sum(egress_bytes), 0)::bigint                AS egress_bytes,
    coalesce(sum(ingress_bytes), 0)::bigint               AS ingress_bytes,
    coalesce(sum(data_sent_bytes), 0)::bigint             AS data_sent_bytes,
    coalesce(sum(data_received_bytes), 0)::bigint         AS data_received_bytes,
    count(*)::bigint                                      AS runs,
    count(*) FILTER (WHERE NOT traffic_measured)::bigint  AS unmeasured_runs
FROM run
WHERE org_id = $1 AND ended_at >= $2 AND ended_at < $3;

-- The same, for one project of it.
-- name: ProjectTrafficInPeriod :one
SELECT
    coalesce(sum(egress_bytes), 0)::bigint                AS egress_bytes,
    coalesce(sum(ingress_bytes), 0)::bigint               AS ingress_bytes,
    coalesce(sum(data_sent_bytes), 0)::bigint             AS data_sent_bytes,
    coalesce(sum(data_received_bytes), 0)::bigint         AS data_received_bytes,
    count(*)::bigint                                      AS runs,
    count(*) FILTER (WHERE NOT traffic_measured)::bigint  AS unmeasured_runs
FROM run
WHERE org_id = $1 AND project_id = $2 AND ended_at >= $3 AND ended_at < $4;

-- How many runs are waiting, for one organization.
--
-- Answered from the partial index, which holds the waiting runs and nothing
-- else: an organization with a million finished runs counts its queue by
-- reading a handful of rows.
-- name: CountQueuedRuns :one
SELECT count(*)::bigint FROM run
WHERE org_id = $1 AND status = 'queued';

-- How many runs were triggered since an instant: by the organization, and by
-- one of its people.
--
-- One statement for both, because a rate limit asks both at the same moment
-- and two round trips would let the answers disagree about what "now" means.
-- name: CountTriggeredSince :one
SELECT
    count(*)::bigint                                    AS by_org,
    count(*) FILTER (WHERE triggered_by = $2)::bigint   AS by_user
FROM run
WHERE org_id = $1 AND created_at >= $3;

-- When the earliest run of a window was created, which is what says how long
-- until a slot frees up.
--
-- The oldest row rather than min(created_at): an aggregate over no rows is
-- null, which sqlc types as an interface or, with a cast, as a value that can
-- never be null — and scanning a null into a time fails at the one moment this
-- is asked. The oldest row simply does not exist when the window is empty, and
-- no rows is an answer the caller already knows how to read.
--
-- Two statements rather than one with a nullable parameter. The trick spelling
-- — $2::uuid IS NULL OR triggered_by = $2 — generates a parameter that cannot
-- be null, so the branch would never fire and the filter would quietly match
-- nobody.
-- name: OldestRunSince :one
SELECT created_at FROM run
WHERE org_id = $1 AND created_at >= $2
ORDER BY created_at
LIMIT 1;

-- name: OldestRunByUserSince :one
SELECT created_at FROM run
WHERE org_id = $1 AND triggered_by = $2 AND created_at >= $3
ORDER BY created_at
LIMIT 1;

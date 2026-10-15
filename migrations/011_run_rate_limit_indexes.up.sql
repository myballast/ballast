BEGIN;

-- What a rate limit reads.
--
-- Counting the runs an organization or a person triggered in the last hour is
-- a question asked on every POST /runs, so it has to be an index lookup rather
-- than a scan of every run ever launched. The existing indexes all carry a
-- project, a scenario or an environment between the organization and the date,
-- which is no use here: the question is about the organization as a whole.
--
-- The per-person window is answered by the same index — the hour's runs are
-- few, and filtering them by who triggered them costs nothing.
CREATE INDEX run_org_created_idx ON run (org_id, created_at DESC);

-- How deep the queue is.
--
-- Partial, and tiny for it: it holds the runs waiting and nothing else, so a
-- table with a million finished runs answers "how many are queued" from an
-- index of a few rows. A run leaves it the moment it starts.
CREATE INDEX run_org_queued_idx ON run (org_id) WHERE status = 'queued';

COMMIT;

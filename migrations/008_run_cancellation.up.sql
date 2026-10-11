BEGIN;

-- When somebody asked for this run to stop.
--
-- A request rather than a verdict, and the distinction is the whole design. A
-- queued run is cancelled on the spot: nothing is running and no target is
-- held. A run being executed cannot be: its generator is loading somebody's
-- environment, and a status saying the run is over while that continues would
-- be false — worse, releasing the target would let the next run measure this
-- one's traffic as well as its own.
--
-- So the API writes here, the worker sees it within a couple of seconds,
-- destroys the container, and only then writes the terminal status and frees
-- the target, in one transaction. The run that nobody is executing — because
-- the worker died — is the collector's business, as before.
ALTER TABLE run ADD COLUMN cancel_requested_at timestamptz;

COMMIT;

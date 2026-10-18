-- Two things the environments screen needs.
--
-- Days of the week for the allowed window. A bit per day, Monday as bit 0, and
-- zero for every day: every window written before this column existed is a
-- window on all seven, which is what zero says, so nothing has to be backfilled
-- and no existing environment changes meaning. A window that crosses midnight
-- belongs to the day it starts; the rule lives in the domain, where the zone and
-- daylight saving can be applied.
--
-- When the target's challenge was last fetched, successful or not. Null until
-- somebody first asks. It is a fact about the attempt, not about the outcome, so
-- it is not tied to the verification status and is kept when an approval is
-- cleared.

BEGIN;

ALTER TABLE environment
    ADD COLUMN allowed_window_days smallint NOT NULL DEFAULT 0,
    ADD COLUMN verification_checked_at timestamptz;

ALTER TABLE environment
    ADD CONSTRAINT environment_window_days_range
        CHECK (allowed_window_days BETWEEN 0 AND 127),
    -- Days narrow a window; without one there is nothing to narrow.
    ADD CONSTRAINT environment_window_days_need_a_window
        CHECK (allowed_window_days = 0 OR allowed_window_start_minute IS NOT NULL);

COMMIT;

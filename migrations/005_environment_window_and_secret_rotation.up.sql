BEGIN;

-- The hours an environment tolerates being loaded.
--
-- Minutes since midnight rather than a time column, and an IANA zone beside
-- them, because a window is a wall-clock rule: "staging is off limits during
-- office hours" means office hours where the people answering the pager are.
-- PostgreSQL's time has no zone, so storing one would mean storing UTC and
-- silently calling it local. Kept as integers, the comparison happens in Go,
-- where the zone can be loaded and daylight saving is somebody's problem rather
-- than nobody's.
--
-- Null means no window at all, which is every environment that exists today.
-- A window that wraps past midnight is expressed with a start greater than an
-- end: 22:00 to 06:00 is 1320 to 360.
ALTER TABLE environment
    ADD COLUMN allowed_window_start_minute smallint,
    ADD COLUMN allowed_window_end_minute   smallint,
    ADD COLUMN allowed_window_tz           text NOT NULL DEFAULT 'UTC';

ALTER TABLE environment
    ADD CONSTRAINT environment_window_start_range
        CHECK (allowed_window_start_minute BETWEEN 0 AND 1439),
    ADD CONSTRAINT environment_window_end_range
        CHECK (allowed_window_end_minute BETWEEN 0 AND 1439),
    -- Half a window is a mistake, not a choice: one bound alone says nothing
    -- about when the other one falls.
    ADD CONSTRAINT environment_window_both_bounds
        CHECK ((allowed_window_start_minute IS NULL) = (allowed_window_end_minute IS NULL)),
    -- A window of zero length would refuse every run, which nobody means. The
    -- way to allow everything is to have no window.
    --
    -- Written as a null check and then a comparison, rather than IS DISTINCT
    -- FROM: NULL IS DISTINCT FROM NULL is false, so that spelling refuses every
    -- environment that has no window at all -- which is all of them today.
    ADD CONSTRAINT environment_window_not_empty
        CHECK (allowed_window_start_minute IS NULL
               OR allowed_window_start_minute <> allowed_window_end_minute),
    ADD CONSTRAINT environment_window_tz_present
        CHECK (btrim(allowed_window_tz) <> '');

-- When a credential was last replaced. The only thing Ballast can show about a
-- secret is its key and its dates, since the value is never read back out, and
-- "this token was set two years ago" is the useful half of that.
--
-- Backfilled from created_at rather than from now(): a row that was never
-- replaced was last written when it was created.
ALTER TABLE secret
    ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();

UPDATE secret SET updated_at = created_at;

-- Row-level security is already on these tables, and a policy covers the row
-- rather than the column, so the columns added here are behind it.

COMMIT;

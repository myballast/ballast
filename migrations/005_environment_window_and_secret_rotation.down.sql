BEGIN;

ALTER TABLE environment
    DROP CONSTRAINT environment_window_start_range,
    DROP CONSTRAINT environment_window_end_range,
    DROP CONSTRAINT environment_window_both_bounds,
    DROP CONSTRAINT environment_window_not_empty,
    DROP CONSTRAINT environment_window_tz_present;

ALTER TABLE environment
    DROP COLUMN allowed_window_start_minute,
    DROP COLUMN allowed_window_end_minute,
    DROP COLUMN allowed_window_tz;

ALTER TABLE secret
    DROP COLUMN updated_at;

COMMIT;

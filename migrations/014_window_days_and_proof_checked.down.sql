-- Reverses 014_window_days_and_proof_checked.up.sql. The days are dropped with
-- the column: a window that was Monday to Friday becomes every day again, which
-- is looser than it was. A rollback past this point should be done knowingly.

BEGIN;

ALTER TABLE environment
    DROP CONSTRAINT IF EXISTS environment_window_days_need_a_window,
    DROP CONSTRAINT IF EXISTS environment_window_days_range,
    DROP COLUMN IF EXISTS verification_checked_at,
    DROP COLUMN IF EXISTS allowed_window_days;

COMMIT;

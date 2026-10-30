-- The oldest worker an organization will still hand a run to.
--
-- A worker says which contract it speaks (runner.contract, see migration 018 and
-- domain.RunnerContract). This is the number it is held to: below it, the worker
-- stays up and takes nothing, and the run it did not take stays queued, intact,
-- for one that is current.
--
-- A column on organization and not a table of its own: there is one number per
-- organization, and organization already carries org_id and its row-level
-- security. It starts at 1, the first contract, so that an organization nobody
-- has configured accepts every worker. It is raised by whoever operates the
-- installation, with the owner role; nothing in the application writes it.

BEGIN;

ALTER TABLE organization
    ADD COLUMN min_runner_contract integer NOT NULL DEFAULT 1
        CHECK (min_runner_contract >= 1);

COMMIT;

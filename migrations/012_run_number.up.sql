-- A run is named by a number within its project: "#1482" says more to a person
-- than a UUID, and is what the breadcrumb, the run list and a message to a
-- colleague all want to say. The UUID stays the identifier; the number is how a
-- run is spoken of.
--
-- The counter lives on the project, and a run takes its number from it as it is
-- inserted. UPDATE ... RETURNING locks the project's row until that transaction
-- ends, so two runs created at the same instant in one project are numbered one
-- after the other, never alike and never with a gap. A run refused before it is
-- written takes no number.
--
-- It is a trigger, not a step of the code that creates a run, because the rule
-- is about the table: every path that will ever insert a run — the API today, a
-- schedule in lot 3 — is numbered, and none can forget to be.
--
-- A counter that only goes up also means a deleted run's number is never
-- handed to another: "#12" does not change what it points at.

BEGIN;

ALTER TABLE project ADD COLUMN run_counter integer NOT NULL DEFAULT 0 CHECK (run_counter >= 0);
ALTER TABLE run ADD COLUMN number integer;

-- The runs that already exist are numbered in the order they were created,
-- which is the order anybody reading the history would give them.
UPDATE run
SET number = numbered.n
FROM (
    SELECT id, org_id, row_number() OVER (PARTITION BY org_id, project_id ORDER BY created_at, id) AS n
    FROM run
) AS numbered
WHERE run.org_id = numbered.org_id AND run.id = numbered.id;

-- Each project's counter picks up where its runs end.
UPDATE project
SET run_counter = counted.last
FROM (SELECT org_id, project_id, max(number) AS last FROM run GROUP BY org_id, project_id) AS counted
WHERE project.org_id = counted.org_id AND project.id = counted.project_id;

ALTER TABLE run ALTER COLUMN number SET NOT NULL;
ALTER TABLE run ADD CONSTRAINT run_number_positive CHECK (number > 0);

CREATE FUNCTION run_assign_number() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.number IS NULL THEN
        UPDATE project SET run_counter = run_counter + 1
        WHERE org_id = NEW.org_id AND id = NEW.project_id
        RETURNING run_counter INTO NEW.number;
    END IF;
    RETURN NEW;
END
$$;

CREATE TRIGGER run_assign_number BEFORE INSERT ON run
    FOR EACH ROW EXECUTE FUNCTION run_assign_number();

-- The unique index is what makes "no two runs alike" true of the database and
-- not only of the code that numbers them. Organization first, like every index.
CREATE UNIQUE INDEX run_org_project_number_idx ON run (org_id, project_id, number);

COMMIT;

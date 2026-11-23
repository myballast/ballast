-- What a pipeline says about itself when it launches a run: which CI, which
-- repository, branch and commit, and the link to the job.
--
-- On the run itself, in five optional columns, and not in a JSON blob or a table of
-- its own: the database can bound a column, and a table of its own would need its
-- own row-level security to say what the run's already says. It describes what
-- launched the run, so it is frozen with the run like the version, the profile and
-- the parameters.
--
-- It is declared by whoever calls and proved to Ballast by nobody. The domain
-- decides what is kept (domain.ParseRunCI), and the constraints below repeat the
-- bounds so that nothing which reaches the table by another road — a script, a
-- migration, a mistake — can store a branch that reads backwards or a link with a
-- password in it.

BEGIN;

ALTER TABLE run
    -- Null is absence. An empty string would be a value that says nothing, and the
    -- two would have to be told apart by whoever reads the column.
    ADD COLUMN ci_provider   text,
    ADD COLUMN ci_repository text,
    ADD COLUMN ci_branch     text,
    ADD COLUMN ci_commit     text,
    ADD COLUMN ci_job_url    text;

-- A closed set, so that two runs from the same provider are written the same way
-- and can be grouped.
ALTER TABLE run
    ADD CONSTRAINT run_ci_provider_check
    CHECK (ci_provider IN ('github_actions', 'gitlab_ci', 'other'));

-- Without a provider there is nothing to say what the rest is about: a repository
-- with no pipeline behind it is a column somebody wrote by hand.
ALTER TABLE run
    ADD CONSTRAINT run_ci_needs_provider
    CHECK (ci_provider IS NOT NULL
        OR (ci_repository IS NULL AND ci_branch IS NULL AND ci_commit IS NULL AND ci_job_url IS NULL));

ALTER TABLE run
    ADD CONSTRAINT run_ci_repository_check
    CHECK (ci_repository IS NULL
        OR (char_length(ci_repository) BETWEEN 1 AND 200
            AND ci_repository ~ '^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$'));

-- Text that reads as what it is. No space, no control character, and none of the
-- invisible ones that move or hide text: the zero-width and directional marks, the
-- line and paragraph separators, the override that makes "release" read backwards,
-- the word joiner and the byte order mark.
ALTER TABLE run
    ADD CONSTRAINT run_ci_branch_check
    CHECK (ci_branch IS NULL
        OR (char_length(ci_branch) BETWEEN 1 AND 255
            AND ci_branch !~ '[[:space:][:cntrl:]\u0085 ​-‏ -‮⁠-⁤﻿]'));

ALTER TABLE run
    ADD CONSTRAINT run_ci_commit_check
    CHECK (ci_commit IS NULL OR ci_commit ~ '^[0-9a-f]{7,64}$');

-- An https address with a host, and nothing a token hides in: no credentials
-- before the host, and nothing after the path.
ALTER TABLE run
    ADD CONSTRAINT run_ci_job_url_check
    CHECK (ci_job_url IS NULL
        OR (char_length(ci_job_url) <= 500
            AND ci_job_url ~ '^https://[^/?#@[:space:][:cntrl:]]+(/[^?#[:space:][:cntrl:]]*)?$'));

COMMIT;

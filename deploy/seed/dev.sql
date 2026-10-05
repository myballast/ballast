-- Development fixtures: one organization, one project, one scenario, one
-- environment. Enough to exercise the chain end to end before scenarios and
-- environments have screens of their own.
--
-- Run as the owner role, which bypasses row-level security. That is why this is
-- a development fixture and not an application code path.
--
-- The target is Ballast's own health endpoint. Pointing a load generator at
-- anything else from a seed script would be pointing it at somebody's server
-- without their knowledge, which is the one thing this product must never do.

BEGIN;

DELETE FROM organization WHERE id = '00000000-0000-4000-8000-000000000001';

INSERT INTO organization (id, name)
VALUES ('00000000-0000-4000-8000-000000000001', 'Ballast development');

-- An approver is required: migration 001 refuses a verified environment that
-- nobody signed off, which is the human step the verification flow rests on.
INSERT INTO app_user (id, org_id, subject, email, name)
VALUES (
    '00000000-0000-4000-8000-000000000005',
    '00000000-0000-4000-8000-000000000001',
    'dev-subject',
    'dev@example.test',
    'Development user'
);

INSERT INTO project (id, org_id, name, slug)
VALUES (
    '00000000-0000-4000-8000-000000000002',
    '00000000-0000-4000-8000-000000000001',
    'Ballast itself',
    'ballast'
);

-- Already verified. The verification flow has its own ticket; a target that
-- has not been through it is refused, and a seed with a pending target could
-- not launch anything.
INSERT INTO environment (
    id, org_id, project_id, name, base_url, verification_token,
    verification_status, verified_at, verified_by, max_vus, max_duration_s
) VALUES (
    '00000000-0000-4000-8000-000000000004',
    '00000000-0000-4000-8000-000000000001',
    '00000000-0000-4000-8000-000000000002',
    'local',
    'http://host.docker.internal:8080',
    'seeded-development-target',
    'verified', now(), '00000000-0000-4000-8000-000000000005', 5, 120
);

INSERT INTO scenario (id, org_id, project_id, name, script, version, default_profile)
VALUES (
    '00000000-0000-4000-8000-000000000003',
    '00000000-0000-4000-8000-000000000001',
    '00000000-0000-4000-8000-000000000002',
    'health check',
    $js$
import http from 'k6/http';
import { check } from 'k6';

// The base URL comes from the environment, never from the script.
export default function () {
	const res = http.get(`${__ENV.BASE_URL}/healthz`);
	check(res, { 'health is 200': (r) => r.status === 200 });
}
$js$,
    1,
    'smoke'
);

INSERT INTO threshold (org_id, scenario_id, metric, expression) VALUES
    ('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000003', 'http_req_failed', 'rate<0.01'),
    ('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000003', 'http_req_duration', 'p(95)<2000');

COMMIT;

\echo ''
\echo 'Seeded. `make dev` points the worker at this organization:'
\echo '  BALLAST_WORKER_ORG_ID=00000000-0000-4000-8000-000000000001'
\echo ''
\echo 'scenario_id    00000000-0000-4000-8000-000000000003'
\echo 'environment_id 00000000-0000-4000-8000-000000000004'

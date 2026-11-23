# Start a run from a signed request

For a tool that is neither GitHub nor GitLab, such as a deploy script or Jenkins. The tool signs a
request with a secret that belongs to one project, and Ballast starts a run. If you use GitHub
Actions or GitLab CI, [Run Ballast from CI](ci.md) is shorter: it has an Action and a template,
and the signing is done for you.

A run started this way meets every guardrail of any other run, in the same place: the target must
be verified, the load is lowered to the environment's ceilings, the allowed window is respected,
and the limits apply. Nothing about a webhook skips one.

## 1. Make the webhook

You need to be an administrator of the project. In the project, open **Webhook** and press
**Create webhook**. Ballast shows you the signing secret **once**: it is stored sealed, where
nothing can read it back. If you lose it, replace it.

The page also shows the address to send requests to, which looks like
`/webhooks/<organization>/<project>`. It goes after the address of your Ballast API, the one the
`ballast` command calls `BALLAST_URL`. It is not a secret and proves nothing.

A project has one webhook. **Replace secret** gives it a new secret and the current one stops
working at once; the address does not change. **Remove webhook** ends it.

## 2. Sign a request

Send a `POST` to the address, with a JSON body and three headers.

The body is the one of `POST /runs`: `scenario_id` and `environment_id` (identifiers, as in the
address of their pages), `profile`, and optionally `params` and `ci`. Both must belong to the
project of the webhook.

| Header | Value |
|---|---|
| `X-Ballast-Timestamp` | The Unix time, in seconds, as digits. |
| `X-Ballast-Delivery` | An identifier that is new for each request: 16 to 64 letters, digits, `-` and `_`. |
| `X-Ballast-Signature` | `v1=` and the lower-case hexadecimal HMAC-SHA256 of the message, keyed with the secret. |

The message is three parts joined by a dot: the timestamp, the delivery and the body, **exactly
as the bytes you send**. If something between you and Ballast rewrites the body, the signature no
longer matches.

A shell example, which needs `openssl` and `curl`:

```sh
body=$(printf '{"scenario_id":"%s","environment_id":"%s","profile":"smoke"}' "$SCENARIO_ID" "$ENVIRONMENT_ID")
timestamp=$(date +%s)
delivery="deploy-$timestamp-$$"
signature=$(printf '%s.%s.%s' "$timestamp" "$delivery" "$body" \
  | openssl dgst -sha256 -hmac "$BALLAST_WEBHOOK_SECRET" -hex | sed 's/^.* //')

curl --fail-with-body --silent --show-error -X POST "$BALLAST_URL$BALLAST_WEBHOOK_PATH" \
  -H "Content-Type: application/json" \
  -H "X-Ballast-Timestamp: $timestamp" \
  -H "X-Ballast-Delivery: $delivery" \
  -H "X-Ballast-Signature: v1=$signature" \
  --data-binary "$body"
```

Here `BALLAST_WEBHOOK_PATH` is the address the page shows. `openssl` takes the secret as an
argument, so it is visible in the list of processes while the command runs: on a shared machine,
sign with the HMAC function of a language instead, and keep the secret in the environment of
the process that does.

## 3. What comes back

A run that is accepted answers `202` with its identifier and its status, as `POST /runs` does.

| Status | What it means |
|---|---|
| `202` | Queued. |
| `400` | The signature held and the request is wrong: the body is not a JSON object, an identifier is not one, or the profile is unknown. The message names the field and never the value. |
| `401` | The request did not prove it holds the secret, or it is not one that can be served now. |
| `403` | The environment is not verified, its window has no room for the run, or the scenario is another project's. |
| `404` | There is no such scenario or environment, or this installation does not serve webhooks. |
| `422` | The scenario cannot be run as it stands. |
| `429` | A limit was reached. `Retry-After` says how many seconds to wait when that is known. |
| `503` | This installation is not accepting runs. |

**Every `401` is the same answer.** A wrong secret, a timestamp that is too old, a request that was
already served, a project that has no webhook and a missing header all read alike, so that nothing
can be learned from them. Ballast's own log says which it was, and never holds the secret or the
signature. If you get one, check the clock of the machine that signs, the secret, and that the
body you signed is the body you sent.

## Rules and limits

| | |
|---|---|
| Clock | The timestamp must be within 5 minutes of Ballast's clock, on either side. |
| Deliveries | A delivery identifier is remembered for 10 minutes, and a request with one that was already served is refused. It is part of what is signed, so it cannot be changed to get a request through again. |
| Body | 64 KiB at most. |
| Runs per hour | 30 per webhook, and 100 for the organization |
| Runs waiting | 20 for the organization |
| Runs at the same time | One per environment: another waits its turn |

A request whose run was refused has spent its delivery, so after a `429` or a `403` sign a new
request with a new identifier.

Requests that are refused create no run, so they never reach the limits above. Keeping a flood of
them out is the job of a reverse proxy in front of Ballast, not of Ballast.

## What is recorded

The run says that the project's webhook started it, and so does every entry of the journal about
it: nobody is named, and nothing says a person did it. Making the webhook, replacing its secret
and removing it are in the journal with who did it, and the secret is in none of them. The runs
and the entries stay after the webhook is removed.

The journal is read by the project's administrators, in the audit of the project.

## Hosting Ballast yourself

Nothing to switch on: the routes are there. The secret is sealed with the key your installation
already has for the secrets of environments, so keep that key as you do now.

# Run Ballast from CI

This page takes you from nothing to a GitHub job that **passes** when your thresholds hold
and **fails** when one is breached. It uses the [GitHub Action](https://github.com/myballast/ballast-action),
which runs the [`ballast` command](cli.md). You need a Ballast project, and a service on the
internet that you own and may load.

## 1. A scenario with a threshold

Open your project in Ballast and make a scenario: write it, import it, or draw it in the
builder. Give it at least one **threshold**, for example `p(95)<500` on `http_req_duration`.
The threshold is what the job judges. A run that judged no threshold is **not** a success,
because it measured nothing against a rule; the job fails unless you say otherwise (see
[Exit codes](#5-read-the-verdict)).

## 2. A target Ballast may load

Ballast only sends load to a target whose ownership you have proved. In the project, open
**Environments** and make one: the base URL of your service (`https://staging.example.com`),
and the most virtual users and the longest run it tolerates.

Then prove you own it, in two steps. First, Ballast shows a challenge. Serve it from **your own
service**, at the root of its origin: the file `/.well-known/ballast-<token>`, with the token as
its whole content. From a shell on the machine that serves the target, for example:

    printf '%s' '<token>' > <web root>/.well-known/ballast-<token>

Press **Verify**. Ballast fetches the file; the content decides, not the status code, since many
servers answer 200 to any path. Second, an **administrator** of the project approves the
verified target. Until then a run against it is refused.

**The target must be reachable from the internet.** Ballast sends the load from its own
servers, and refuses a loopback, private, link-local (such as `169.254.169.254`),
unique-local or multicast address, with no setting to allow one. A service on your laptop or
inside a private network cannot be a target. If you host Ballast yourself, see
[Hosting Ballast yourself](#hosting-ballast-yourself).

A run is also refused outside the **time window** you gave the environment, and a run that
would still be going when the window closes is refused too, not shortened.

## 3. A token, and where to put it

In the project, open **CI tokens** and press **Create token**. You need to be an administrator
of the project. The token:

- belongs to **one project**, and can only view and launch runs there; it can cancel a run it
  launched, and nothing else;
- expires: after 90 days unless you choose another time, one year at most. A project holds 20
  live tokens;
- is shown **once**. Copy it then. Ballast keeps only a hash.

In your GitHub repository, under **Settings → Secrets and variables → Actions**:

| Kind | Name | Value |
|---|---|---|
| Secret | `BALLAST_TOKEN` | the token |
| Variable | `BALLAST_URL` | the address of your Ballast, for example `https://ballast.example.com` |
| Variable | `BALLAST_SCENARIO_ID` | the identifier of the scenario |
| Variable | `BALLAST_ENVIRONMENT_ID` | the identifier of the environment |

An identifier is the last part of the address of the page: `/projects/<project>/scenarios/<id>`
for a scenario, `/projects/<project>/environments/<id>` for an environment. The token goes in a
secret. The rest is not secret.

## 4. Add the workflow

A complete example, with a small service to load and a workflow that shows a job that passes
and one that fails, is in [ballast-demo](https://github.com/myballast/ballast-demo).

Add `.github/workflows/load-test.yml`:

```yaml
name: Load test

on:
  pull_request:
  workflow_dispatch:

permissions:
  contents: read

jobs:
  smoke:
    runs-on: ubuntu-latest
    steps:
      - uses: myballast/ballast-action@v1
        with:
          url: ${{ vars.BALLAST_URL }}
          token: ${{ secrets.BALLAST_TOKEN }}
          scenario: ${{ vars.BALLAST_SCENARIO_ID }}
          environment: ${{ vars.BALLAST_ENVIRONMENT_ID }}
          profile: smoke
```

No checkout is needed: the Action does not read your repository. `profile` is `smoke`, `load`,
`stress`, `spike` or `soak`. Push it, or run it from the **Actions** tab.

A workflow started by a pull request **from a fork** is not given your secrets, and the step
fails saying the token is empty. That is intended. Do not work around it with
`pull_request_target`, which would run the fork's code with your secrets.

## 5. Read the verdict

The job log shows the output of the run as it happens: what k6 prints, with a progress line every
second (a `smoke` run lasts about a minute), then the thresholds:

    Thresholds:
      met       http_req_duration  p(95)<500
      BREACHED  http_req_failed    rate<0.01
    Verdict: breached
    Run: https://ballast.example.com/runs/…

The same table is on the summary page of the job, with a link to the run when Ballast has an
`https` address for its pages. A threshold is
always `met` or `BREACHED`, in words: a log has no colour.

The step leaves with a code, and the job follows it:

| Code | `verdict` output | Meaning |
|---|---|---|
| 0 | `met` | The run finished and its thresholds held. The job passes. |
| 1 | `breached` | The run finished and a threshold was breached. The job fails. |
| 2 | `none`, or empty | Anything else: an error, a refusal, a run that did not finish, a cancellation, a run that judged no threshold. The job fails. |

Two inputs change what counts as a success, and only these: `no-fail-on-breach: "true"` lets a
breach pass (it is still shown, and still in the `verdict` output), and
`allow-no-threshold: "true"` accepts a run that judged none. Neither excuses an error.

To **see both outcomes**, run the same workflow against a scenario whose threshold is easy to
hold (`p(95)<2000` on a fast endpoint), then against one it cannot hold (`p(95)<1`). The
first job is green and the second is red, with `BREACHED` in its log.

The step has outputs, `run-id`, `status`, `verdict`, `url` and `exit-code`, to use in later
steps; the [README of the Action](https://github.com/myballast/ballast-action#outputs) shows how.

## GitLab CI

The same thing, as a file to include. Steps 1 to 3 are the same: a scenario with a threshold, a
target Ballast may load, and a token. In **Settings → CI/CD → Variables** make:

| Name | Value | Options |
|---|---|---|
| `BALLAST_TOKEN` | the token | **Masked** and **Protected** |
| `BALLAST_URL` | the address of your Ballast, for example `https://ballast.example.com` | |
| `BALLAST_SCENARIO_ID` | the identifier of the scenario | |
| `BALLAST_ENVIRONMENT_ID` | the identifier of the environment | |
| `BALLAST_PROFILE` | `smoke`, `load`, `stress`, `spike` or `soak` | |

Then add to `.gitlab-ci.yml`:

```yaml
include:
  - remote: https://raw.githubusercontent.com/myballast/ballast/v1.2.3/ops/gitlab/ballast.gitlab-ci.yml

ballast-load-test:
  rules:
    - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH
  resource_group: ballast-staging
  timeout: 3h
```

Use the tag of a release in the address, never a branch: the file is the job, and a job that
changes without a commit of yours is one nobody can explain. The file carries the version of the
command that goes with it, and downloads that binary from the release, checks it against the
sums of the release, and refuses it if they do not match or if it does not say the version
that was asked for. To pin the command to another version, set `BALLAST_VERSION`.

The job fails with the exit code of the command, so a threshold that is breached fails the
job (code 1), and so does anything else that is not a success (code 2). Optional variables:

| Name | Meaning |
|---|---|
| `BALLAST_TIMEOUT` | How long to give the run from its launch, for example `90m`. Two hours by default: set the `timeout` of the job above it, as in the example. |
| `BALLAST_QUEUE_TIMEOUT` | How long to wait, in all, when a limit is reached. Ten minutes by default, `0` fails at the first refusal. |
| `BALLAST_NO_FAIL_ON_BREACH` | `true` lets a breach pass. It is still shown. |
| `BALLAST_ALLOW_NO_THRESHOLD` | `true` accepts a run that judged no threshold. |

To let a breach show as a warning without failing the pipeline, use GitLab's own
`allow_failure: { exit_codes: 1 }` on the job: an error still fails it.

Some things that GitLab does and that explain a failure:

- A **protected** variable is only given to pipelines of protected branches and tags, and no
  variable is given to a merge request from a fork. The job then stops saying `BALLAST_TOKEN` is
  empty. That is intended. If the pipeline of a feature branch must run, protect the branch or
  make the variable unprotected, knowing what that gives to everybody who can push there.
- A `$` in the value of a variable is read by GitLab as the start of a reference: write `$$`.
- Two jobs that load the same environment wait for each other on Ballast's side, and a
  `resource_group` keeps them from starting together.
- The job runs in a Linux image (Alpine, pinned by digest) and needs to reach the release on
  GitHub and your Ballast. A Windows runner is not supported by this file.

There is no job summary or output as in GitHub: the verdict is in the log and in the exit code.
The command still tells Ballast which pipeline, branch and commit the run came from.

## Limits

| | |
|---|---|
| Virtual users | 200 at most, and no more than the environment says |
| Length of a run | 1 hour at most, and no more than the environment says |
| Runs per hour | 30 per token, 100 for the organization |
| Runs waiting | 20 for the organization |
| Runs at the same time | one per environment: another waits its turn |

A profile that asks for more than the environment allows is **lowered to it**, not refused, and
what runs is what the run page says. When a limit is reached the step fails and says which one,
and how long to wait when that is known. `timeout` (two hours by default) counts from the
launch, so it includes the time spent waiting in the queue; when it is up, the run is asked to
stop and the step fails.

### When a limit is reached

A launch refused for a limit has created nothing, so the tool asks again, for up to
`queue-timeout` (ten minutes by default) in all. It waits as long as the server says when the
server says (a window of runs per hour empties at a known moment), and otherwise a little
longer each time, up to a minute. If the server asks for more than what is left of
`queue-timeout`, the step fails at once and says so, rather than waiting to fail anyway.
`queue-timeout: 0` fails at the first refusal. A launch refused for anything else (an
unverified target, a closed window, a bad token) is never asked again.

A run that is accepted but waits behind another run on the same environment says "still
queued" every minute. To avoid the wait, give each pipeline its own environment, and keep two
jobs of a workflow from loading the same one together:

    concurrency:
      group: ballast-staging
      cancel-in-progress: false

## When it does not work

| The step reports | What it means |
|---|---|
| the token input is empty | The secret is missing or not given to this workflow, which is what a pull request from a fork looks like. |
| Ballast refused the token | It was revoked or it expired. Make a new one. |
| refused, the environment is not verified | Step 2 is not finished: the proof was not served, or nobody has approved it. |
| refused, outside the window | The environment only accepts load at certain hours. |
| limiting runs | One of the limits above. The message names it. |
| not accepting runs | The operator has closed the door for now. |
| a scenario or an environment that is not found | The identifier belongs to another project than the token's. |
| the run errored, or timed out | The run started and did not finish. Its page says why. |

## Hosting Ballast yourself

`make selfhost` runs the whole application with one command (see the README). Self-hosting
changes nothing about the rule above: **a target must be reachable from the machine that runs
the load**, which is the machine of the Ballast worker, and a private address is refused.

- A target on the internet works as it does on the hosted service.
- A target inside your private network does not, and there is no setting for it in
  production.
- On a developer's machine only, the worker accepts a list of networks that you name
  (`BALLAST_EGRESS_ALLOWED_CIDRS`), because a run launched there aims at a server on the same
  machine. That list is empty in production, and loopback and link-local addresses are
  refused whatever it says.
- The GitHub runner must reach **your Ballast**: `url` has to be an address it can open. A
  Ballast on `localhost` cannot be reached from a GitHub-hosted runner.

## Security, in short

- The token is read from the environment and never printed or passed as an argument. The Action
  masks it in the log, and the secret store of GitHub does too.
- A token reaches one project and cannot create other tokens, read secrets or stop other
  people's runs. Revoking it from **CI tokens** takes effect on its next request.
- The Action downloads the `ballast` command of one version, checks its sum against the one of
  the release, and refuses anything else. The sums say a download is whole, not that a release
  is genuine; [`cli.md`](cli.md) says how to rebuild a release and compare.

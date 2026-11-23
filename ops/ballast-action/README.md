# Ballast load test, a GitHub Action

Starts a load test run on [Ballast](https://github.com/myballast/ballast), shows its output
in the log of the job, waits for it to end, and fails the job when a threshold of the
scenario is breached.

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

No checkout is needed: the Action does not read your repository.

## Before you use it

1. In Ballast, open the project's **CI tokens** page and create a token. It belongs to one
   project, can view and launch runs, and expires. Copy it once, when it is shown.
2. In your repository, add it as a **secret** named `BALLAST_TOKEN`. The address of your
   Ballast and the identifiers of the scenario and of the environment are not secret: a
   repository **variable** is the right place.
3. The environment must have been **verified** in Ballast. A run against one that has not
   is refused, and so is a run that goes beyond what the environment allows. The target must
   also be **reachable from the internet**: Ballast sends the load from its own servers, and
   refuses a private, loopback or link-local address, with no setting to allow one.

The whole way, from nothing to a passing and a failing job, is in
[Run Ballast from CI](https://github.com/myballast/ballast/blob/main/docs/ci.md).

Secrets are not given to a workflow started by a pull request from a fork, so the step
fails there with a message that says the token is empty. Do not "fix" it with
`pull_request_target`, which would run code from the fork with your secrets.

## Inputs

| Input | Required | Default | |
|---|---|---|---|
| `token` | yes | | An API token of the project. Give it a secret, never a value written in the workflow. |
| `url` | yes | | The address of your Ballast, for example `https://ballast.example.com`. |
| `scenario` | yes | | The identifier of the scenario to run (a UUID). |
| `environment` | yes | | The identifier of the environment to run against (a UUID). |
| `profile` | yes | | The load profile: `smoke`, `load`, `stress`, `spike` or `soak`. |
| `timeout` | no | two hours | How long to give the run from its launch, for example `90m`. When it is up the run is asked to stop and the step fails. |
| `allow-no-threshold` | no | `false` | Accept a run that judged no threshold. Without it such a run fails the step: it measured nothing against a rule. |
| `no-fail-on-breach` | no | `false` | Do not fail the step when a threshold is breached. The breach is still shown, and still in the `verdict` output. |
| `version` | no | the Action's own | The version of the `ballast` command to use, for example `v1.2.3`. |

There is no `project` input: a token belongs to one project, and a run belongs to the project
of its scenario.

## Outputs

| Output | |
|---|---|
| `run-id` | The identifier of the run. Empty when none was launched. |
| `status` | The last known status of the run. |
| `verdict` | `met`, `breached` or `none`. Empty when the run did not finish: nothing was judged. |
| `url` | The address of the run's page. Empty unless it is an `https` address. |
| `exit-code` | The code the command left with: 0, 1 or 2. |

The step also writes a short summary of the thresholds to the page of the job.

The step fails, with the code of the command, when the run did not succeed: `1` when a
threshold was breached, `2` for anything else (an error, a refusal, a run that did not
finish, a cancellation, a run that judged no threshold). To read the outputs of a step that
failed, let it fail without failing the job, and decide afterwards:

```yaml
      - id: ballast
        continue-on-error: true
        uses: myballast/ballast-action@v1
        with:
          url: ${{ vars.BALLAST_URL }}
          token: ${{ secrets.BALLAST_TOKEN }}
          scenario: ${{ vars.BALLAST_SCENARIO_ID }}
          environment: ${{ vars.BALLAST_ENVIRONMENT_ID }}
          profile: load

      - name: Say how it went
        env:
          VERDICT: ${{ steps.ballast.outputs.verdict }}
          RUN_URL: ${{ steps.ballast.outputs.url }}
        run: echo "verdict ${VERDICT:-none}, $RUN_URL"

      - if: steps.ballast.outcome == 'failure'
        run: exit 1
```

Pass outputs to a script through `env:`, as above, and do not write them into the script with
`${{ }}`: they come from a server, and a value written into a script is code.

## What the Action does

It downloads the `ballast` command of a release from
[the releases of Ballast](https://github.com/myballast/ballast/releases), over https, for the
runner's system (Linux, macOS or Windows; x64 or arm64). It checks the download against the
`checksums.txt` of the release and refuses it if the sum is not the one listed. It then runs
`ballast version` to be sure it is the version it asked for, and runs the command. The token
is given to the command in its environment and never as an argument.

The checksums are published in the same release as the binaries: they say the download is
whole, and are not a signature. How to rebuild a release and compare is in
[`docs/cli.md`](https://github.com/myballast/ballast/blob/main/docs/cli.md) of Ballast.

When the job is cancelled, the command is told and asks Ballast to stop the run. A run that is
under way is stopped by the worker running it a few seconds later, so look at the run's page
to see that it has.

## Versions

- `myballast/ballast-action@v1` is the latest `v1.x.y`. It moves, and every version it moves
  to uses a known `ballast` command: the one named in this repository's `VERSION` file, not
  "the latest one".
- `myballast/ballast-action@v1.2.3` is one release of the Action and never changes.
- A commit hash is the strictest way to pin it, and the one GitHub recommends for actions
  you do not own.
- `version:` chooses another `ballast` command than the Action's own. It must be a version
  of Ballast's releases, written like `v1.2.3` or `v1.2.3-rc.1`: `latest` does not exist.

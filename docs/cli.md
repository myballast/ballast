# The `ballast` command line tool

`ballast` starts a load test run on Ballast from a pipeline, follows its output, waits for
the run to end, and leaves with an exit code the pipeline can act on. It is a single
static binary with no dependency, and it is what the GitHub Action and the GitLab template
run.

## Install

Every release of Ballast publishes one binary per system, and the sums to check them with,
on the [releases page](https://github.com/myballast/ballast/releases):

| System | File |
|---|---|
| Linux, x86-64 | `ballast_linux_amd64` |
| Linux, ARM64 | `ballast_linux_arm64` |
| macOS, Intel | `ballast_darwin_amd64` |
| macOS, Apple silicon | `ballast_darwin_arm64` |
| Windows, x86-64 | `ballast_windows_amd64.exe` |
| Windows, ARM64 | `ballast_windows_arm64.exe` |
| Sums of all of the above | `checksums.txt` |

The names do not change from one release to the next, so a pipeline can pin a version by
its tag alone. For example, to download the Linux binary of version `vX.Y.Z` with its sums:

    curl -fsSLO https://github.com/myballast/ballast/releases/download/vX.Y.Z/ballast_linux_amd64
    curl -fsSLO https://github.com/myballast/ballast/releases/download/vX.Y.Z/checksums.txt

Check the download (below), make it executable with `chmod +x ballast_linux_amd64`, and ask
it which version it is with `./ballast_linux_amd64 version`.

Until a first release is published, build it yourself with `make build-cli`, which writes
`bin/ballast`.

## Check the download

Every binary of a release is listed with its SHA-256 sum in `checksums.txt`. Put the binary
you downloaded and `checksums.txt` in the same folder, then run the command for your system.
It prints `<file>: OK` and succeeds when the file is the one that was released. Anything
else, no output included, means it does not verify: do not run it.

Linux:

    grep ' ballast_linux_amd64$' checksums.txt | sha256sum -c - | grep ': OK$'

macOS:

    grep ' ballast_darwin_arm64$' checksums.txt | shasum -a 256 -c - | grep ': OK$'

Windows (PowerShell):

    $expected = ((Select-String -Path checksums.txt -Pattern 'ballast_windows_amd64.exe$').Line -split '  ')[0]
    $actual = (Get-FileHash ballast_windows_amd64.exe -Algorithm SHA256).Hash.ToLower()
    if ($expected -and $actual -eq $expected) { 'OK' } else { 'DOES NOT VERIFY' }

For another system, use the name of the file you downloaded. The final `grep` is part of the
check: some versions of `sha256sum` answer success when there is nothing to check, which is
what happens when the file you downloaded is not in the list.

**What this proves, and what it does not.** The sums protect against a damaged or incomplete
download. They are published in the same release as the binaries, so they are not a
signature: they cannot show that the release itself is genuine. If that matters to you, build
the tool from the tag and compare, as below.

## Build it yourself, and compare

The release builds are reproducible. Building a given tag with the Go version `go.mod`
names gives the same bytes, from any directory and whatever is in the build cache, and
every release compiles each binary twice and stops if the two differ. So you can check a
release against the source:

    git checkout vX.Y.Z
    make release-cli VERSION=vX.Y.Z

and compare `dist/checksums.txt` with the one of the release. The file names and the
flags are those of the release; `dist/` is not tracked by git.

## Versions

A release is a git tag of the form `vMAJOR.MINOR.PATCH`, for example `v0.1.0`. A tag with a
hyphen, such as `v0.1.0-rc.1`, is published as a pre-release. `ballast version` says which
one a binary is, and what it was built for:

    ballast v0.1.0 (linux/amd64, go1.23.5)

A binary that was not built by a release says `dev`.

## Use

Two settings are read from the environment, and nowhere else. In a pipeline they come from
the CI system's secrets:

| | |
|---|---|
| `BALLAST_URL` | The address of your Ballast, for example `https://ballast.example.com`. Plain `http` is accepted only for `localhost`. |
| `BALLAST_TOKEN` | An API token of the project, made in its "CI tokens" page. It is never an argument: an argument shows in a process list and in the log of the pipeline that typed it. |

Then:

    ballast run --scenario ID --environment ID --profile smoke

`--scenario` and `--environment` are the identifiers of the scenario and of the environment,
and the environment must have been verified in Ballast first: a run against one that has not
is refused. There is no `--project`: a token belongs to one project, and a run belongs to the
project of its scenario. `--timeout` gives the run a time, two hours by default; when it is
up, or when the tool is interrupted, the run is asked to stop. `--no-fail-on-breach` and
`--allow-no-threshold` change what counts as a success, and `ballast help` lists everything.

The tool leaves with:

| Code | Meaning |
|---|---|
| 0 | The run finished and its thresholds held. |
| 1 | The run finished and a threshold was breached. |
| 2 | Anything else: an error, a refusal, a run that did not finish, a cancellation, a run that judged no threshold. |

## GitHub Actions

Under GitHub Actions the tool also writes what the next steps can read. When the runner names
the files (`GITHUB_OUTPUT` and `GITHUB_STEP_SUMMARY`) it adds the outputs `run-id`, `status`,
`verdict`, `url` and `exit-code`, and a short summary of the thresholds to the page of the
job. `verdict` is `met`, `breached` or `none`, and it is empty when the run did not finish:
nothing was judged. A file that cannot be written is a warning and never changes the code the
tool leaves with.

You do not have to install the tool yourself. The Action does it, checks the download against
the sums of the release, and runs it:

```yaml
name: Load test

on:
  pull_request:

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

The token is a secret of the repository. A workflow started by a pull request from a fork is
not given secrets, and the step says so when it finds the token empty. The inputs, the
outputs and how to pin a version are in the README of
[the Action](https://github.com/myballast/ballast-action), and the whole way, from a token
to a failing job, is in [Run Ballast from CI](ci.md).

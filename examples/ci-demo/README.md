# Ballast demo: run a load test from CI

A small service to load, two scenarios, and a workflow with two jobs: one that passes
because its threshold holds, and one that shows what a breached threshold looks like.
It is the example of [Run Ballast from CI](https://github.com/myballast/ballast/blob/main/docs/ci.md),
which goes through each step in detail.

## What is here

| | |
|---|---|
| `main.go`, `server.go` | The target: `/health` and `/items` answer at once, `/slow?ms=800` answers after 800 ms (3000 at most), and `/.well-known/ballast-<token>` serves the proof that you own it. Standard library only. |
| `Dockerfile` | The service as a static binary in an empty image, run as nobody. |
| `scenarios/holds.js` | Asks for `/health` and `/items`. |
| `scenarios/breaches.js` | Asks for `/slow?ms=800`. |
| `.github/workflows/load-test.yml` | The two jobs. |

## Set it up

1. **Put the service on the internet.** Ballast sends its load from its own servers, so a
   service on your laptop cannot be loaded. Any host that runs a container does:

        docker build -t ballast-demo .
        docker run -p 8080:8080 -e BALLAST_CHALLENGE_TOKEN=<token, below> ballast-demo

2. **Make the environment in Ballast.** In your project, open **Environments**, make one with
   the address of the service, and press **Verify**: Ballast shows a token. Set it as
   `BALLAST_CHALLENGE_TOKEN` on the service and restart it, so that it serves
   `/.well-known/ballast-<token>`. Press **Verify** again, then have an administrator approve
   the target.
3. **Make the two scenarios.** Make a scenario of each file in `scenarios/`, and give both the
   same threshold:

   | Metric | Rule |
   |---|---|
   | `http_req_duration` | `p(95)<500` |

4. **Make a token.** In the project, open **CI tokens** and create one.
5. **Set the repository's secrets and variables** (Settings → Secrets and variables → Actions):

   | Kind | Name | Value |
   |---|---|---|
   | Secret | `BALLAST_TOKEN` | the token |
   | Variable | `BALLAST_URL` | the address of your Ballast |
   | Variable | `BALLAST_ENVIRONMENT_ID` | the identifier of the environment |
   | Variable | `BALLAST_SCENARIO_HOLDS_ID` | the identifier of the scenario made from `holds.js` |
   | Variable | `BALLAST_SCENARIO_BREACHES_ID` | the identifier of the scenario made from `breaches.js` |

   An identifier is the last part of the address of the page of the scenario or of the
   environment.

## Run it

Open the **Actions** tab, choose **Load test**, and press **Run workflow**.

- `holds` is green. Its log ends with `met` and the link to the run.
- `breaches` shows `BREACHED` next to `http_req_duration`, and the verdict `breached`. The
  job is green because the example expects it. To see a red job, delete the line
  `continue-on-error: true` from the workflow and run it again: the step fails with exit code 1.

## Recording it

The sequence that shows the whole thing in two minutes: the service answering in a browser,
**Verify** in Ballast turning green, the two scenarios with their threshold, **Run workflow**,
then the two jobs, then the `BREACHED` line in the log of the second and the table on the
summary page of the job.

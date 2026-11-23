#!/usr/bin/env bash
#
# Runs the ballast command for a GitLab job.
#
# The settings are CI/CD variables of the job (BALLAST_*), read here from the environment:
# nothing a pipeline's variables hold is ever written into a script, because a value that
# is text from somebody would then be code. Each one becomes an argument of its own in the
# form --name=value, so that a space, a quote, "$(…)" or a leading dash in it can be
# nothing but part of a value.
#
# The token is BALLAST_TOKEN, in the environment, and the command reads it from there. It
# is never an argument: an argument shows in a list of processes.
#
# The command is started with exec. It is the process of this script, and the signal a
# runner sends to a job that is cancelled reaches it, which is what asks the run to stop.

set -euo pipefail
# A value that holds a * is a value, and not a list of files.
set -f
export LC_ALL=C

die() {
  echo "ballast-gitlab: $*" >&2
  exit 1
}

bin="${BALLAST_BIN:-}"
[ -n "$bin" ] && [ -f "$bin" ] && [ -x "$bin" ] || die "there is no command to run: the install step did not leave one"

[ -n "${BALLAST_TOKEN:-}" ] || die "BALLAST_TOKEN is empty. Make a CI/CD variable of that name that holds an API token of the project, masked and protected. A protected variable is only given to a pipeline of a protected branch or tag, and none is given to a merge request from a fork, so this is also what it looks like there"
[ -n "${BALLAST_URL:-}" ] || die "BALLAST_URL is empty. Give it the address of your Ballast, for example https://ballast.example.com"

args=(
  run
  "--scenario=${BALLAST_SCENARIO_ID:-}"
  "--environment=${BALLAST_ENVIRONMENT_ID:-}"
  "--profile=${BALLAST_PROFILE:-}"
)

if [ -n "${BALLAST_TIMEOUT:-}" ]; then
  args+=("--timeout=${BALLAST_TIMEOUT}")
fi
if [ -n "${BALLAST_QUEUE_TIMEOUT:-}" ]; then
  args+=("--queue-timeout=${BALLAST_QUEUE_TIMEOUT}")
fi

# flag_if adds $2 when the variable named $3 holds $1 = "true", and refuses what is
# neither "true" nor "false" nor empty, naming the variable and never its value.
flag_if() {
  case "$1" in
    true) args+=("$2") ;;
    false | "") ;;
    *) die "the variable $3 must be true or false" ;;
  esac
}
flag_if "${BALLAST_ALLOW_NO_THRESHOLD:-}" "--allow-no-threshold" "BALLAST_ALLOW_NO_THRESHOLD"
flag_if "${BALLAST_NO_FAIL_ON_BREACH:-}" "--no-fail-on-breach" "BALLAST_NO_FAIL_ON_BREACH"

exec "$bin" "${args[@]}"

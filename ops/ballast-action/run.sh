#!/usr/bin/env bash
#
# Runs the ballast command for the step that holds the inputs of the workflow.
#
# The inputs arrive in the environment (BALLAST_ARG_*), written there by the workflow
# and read here: nothing the workflow's context holds is ever written into a script,
# because a value that is text from somebody would then be code. Each one becomes an
# argument of its own in the form --name=value, so that a space, a quote, "$(…)" or a
# leading dash in it can be nothing but part of a value.
#
# The token is BALLAST_TOKEN, in the environment of the step, and the command reads it
# from there. It is never an argument: an argument shows in a list of processes.
#
# The command is started with exec. It is the process of the step, and the signal a
# runner sends to a job that is stopped reaches it, which is what asks the run to stop.

set -euo pipefail
# A value that holds a * is a value, and not a list of files.
set -f
export LC_ALL=C

die() {
  echo "ballast-action: $*" >&2
  exit 1
}

bin="${BALLAST_BIN:-}"
[ -n "$bin" ] && [ -f "$bin" ] && [ -x "$bin" ] || die "there is no command to run: the install step did not leave one"

# Said in the words of the workflow, which are the inputs of the Action.
[ -n "${BALLAST_TOKEN:-}" ] || die "the token input is empty. Give it a secret that holds an API token of the project, for example \${{ secrets.BALLAST_TOKEN }}. A workflow started by a pull request from a fork is not given secrets, so this is also what it looks like there"
[ -n "${BALLAST_URL:-}" ] || die "the url input is empty. Give it the address of your Ballast, for example https://ballast.example.com"

args=(
  run
  "--scenario=${BALLAST_ARG_SCENARIO:-}"
  "--environment=${BALLAST_ARG_ENVIRONMENT:-}"
  "--profile=${BALLAST_ARG_PROFILE:-}"
)

if [ -n "${BALLAST_ARG_TIMEOUT:-}" ]; then
  args+=("--timeout=${BALLAST_ARG_TIMEOUT}")
fi

if [ -n "${BALLAST_ARG_QUEUE_TIMEOUT:-}" ]; then
  args+=("--queue-timeout=${BALLAST_ARG_QUEUE_TIMEOUT}")
fi

# flag_if adds $2 when the input named $1 (written $3) is "true", and refuses what is
# neither "true" nor "false" nor empty, naming the input and never its value.
flag_if() {
  case "$1" in
    true) args+=("$2") ;;
    false | "") ;;
    *) die "the input $3 must be true or false" ;;
  esac
}
flag_if "${BALLAST_ARG_ALLOW_NO_THRESHOLD:-}" "--allow-no-threshold" "allow-no-threshold"
flag_if "${BALLAST_ARG_NO_FAIL_ON_BREACH:-}" "--no-fail-on-breach" "no-fail-on-breach"

# Tell the runner to mask the token in the log of the job. It already masks a secret, and
# this covers a token that was not one. Only a token on a single line: what follows a line
# break would be read as another workflow command.
case "$BALLAST_TOKEN" in
  *$'\n'* | *$'\r'*) ;;
  *) echo "::add-mask::${BALLAST_TOKEN}" ;;
esac

exec "$bin" "${args[@]}"

#!/usr/bin/env bash
#
# Verifies the files of a release against their sums: ops/release/verify.sh DIR
#
# It is what stands between a directory and a published release, and the checksum tools
# a machine has are more forgiving than that: some answer "fine" to a file with no sum
# in it. So the sums are read here, and the tool is asked for the arithmetic only. The
# directory is right when, and only when:
#
#   * checksums.txt is there, every line of it is "<64 lowercase hex>  <name>", with two
#     spaces and a name of the form a release uses, and no line has a carriage return;
#   * the names it lists are exactly the other files of the directory, each once: no
#     file is unlisted, no line is for a file that is not there, nothing else is in it;
#   * the tool says "<name>: OK" for every one of them, and nothing else.

set -euo pipefail
set -f
export LC_ALL=C

die() {
  echo "release: $*" >&2
  exit 1
}

dir="${1:-}"
[ -n "$dir" ] && [ -d "$dir" ] || die "there is no directory to verify"
[ -f "$dir/checksums.txt" ] || die "checksums.txt is not there"

pattern='^[0-9a-f]{64}  (ballast_[a-z0-9_]+(\.exe)?)$'
listed=""
while IFS= read -r line || [ -n "$line" ]; do
  [[ "$line" =~ $pattern ]] || die "a line of checksums.txt is not \"<sum>  <file>\""
  listed="$listed${BASH_REMATCH[1]}"$'\n'
done <"$dir/checksums.txt"
[ -n "$listed" ] || die "checksums.txt lists nothing"

# Each file once, and no file left out or left over: the sorted names are compared as
# they are, so a duplicate is a difference.
listed="$(printf '%s' "$listed" | sort)"
present="$(ls -A "$dir" | grep -vx 'checksums.txt' | sort || true)"
[ "$listed" = "$present" ] || die "the sums and the files of the directory are not the same files"

if command -v sha256sum >/dev/null 2>&1; then
  output="$(cd "$dir" && sha256sum -c checksums.txt 2>&1)" || die "the sums do not verify the files"
else
  output="$(cd "$dir" && shasum -a 256 -c checksums.txt 2>&1)" || die "the sums do not verify the files"
fi

# Success is seen, and not assumed from a status.
for name in $listed; do
  printf '%s\n' "$output" | grep -qx "$name: OK" || die "$name was not verified"
done

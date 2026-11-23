#!/usr/bin/env bash
#
# Installs the ballast command line tool on a GitLab runner, for the job that runs it.
#
# It is the install step of the GitHub Action (ops/ballast-action/install.sh) with the
# runner's own words replaced: the system and architecture are read from uname, and the
# path of the command is printed on stdout (messages go to stderr), so that a job can
# write BALLAST_BIN="$(bash install.sh)". A test holds the two to the same refusals.
#
# What it does, in this order, and stops at the first thing that is wrong:
#
#   1. reads the version (BALLAST_VERSION). "latest" does not exist: a pipeline that
#      changed under its owner without a commit would be one nobody can explain.
#   2. maps the runner's system and architecture to the file a release has for it.
#   3. downloads that file and the sums of the release, over https only.
#   4. checks the sum, reading the sums itself. Checksum tools answer "success" to a
#      list that is empty, so none of them is asked.
#   5. runs `ballast version` and checks it says the version that was asked for.
#
# The sums are in the same release as the binaries: they say the download is whole, and
# not that the release is genuine. docs/cli.md says how to rebuild a tag and compare.
#
# BALLAST_DOWNLOAD_BASE exists for the tests of this script. It is accepted only when it
# names this machine (127.0.0.1 or localhost, with a port): nothing a pipeline sets can
# send the download to another server.
#
# Written for the bash that macOS ships (3.2) as much as for the one on a Linux image.

set -euo pipefail
# A name that holds a * is a name, and not a list of files.
set -f
export LC_ALL=C

die() {
  echo "ballast-gitlab: $*" >&2
  exit 1
}

# valid_version is the one of ops/release/build.sh in the repository of the tool, and a
# test there holds the two to the same answers: SemVer with a leading v, at most 64
# characters, no build metadata. The characters are checked first, on their own, so
# that nothing that is not part of a version reaches the pattern.
valid_version() {
  local version="$1"
  [ "${#version}" -ge 1 ] && [ "${#version}" -le 64 ] || return 1
  case "$version" in
    *[!0-9A-Za-z.-]*) return 1 ;;
  esac
  local identifier='(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)'
  local pattern="^v(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)(-${identifier}(\\.${identifier})*)?\$"
  [[ "$version" =~ $pattern ]]
}

version="${BALLAST_VERSION:-}"
[ -n "$version" ] || die "BALLAST_VERSION is empty: give it a version, for example v1.2.3"
valid_version "$version" || die "that is not a version: write it like v1.2.3 or v1.2.3-rc.1, at most 64 characters"

# The file of the release for this runner. Only the names the runner gives itself are
# read, and only these exact ones.
case "$(uname -s)" in
  Linux) goos=linux ;;
  Darwin) goos=darwin ;;
  *) die "this system is not supported: a release has binaries for Linux and macOS here (a Windows runner needs the binary from the release page, run by hand)" ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) goarch=amd64 ;;
  aarch64 | arm64) goarch=arm64 ;;
  *) die "this architecture is not supported: a release has binaries for x64 and arm64" ;;
esac
extension=""
file="ballast_${goos}_${goarch}${extension}"

default_base="https://github.com/myballast/ballast/releases/download"
base="${BALLAST_DOWNLOAD_BASE:-$default_base}"
protocols="=https"
if [ "$base" != "$default_base" ]; then
  loopback='^http://(127\.0\.0\.1|localhost):[0-9]{1,5}(/[A-Za-z0-9._/-]*)?$'
  [[ "$base" =~ $loopback ]] || die "BALLAST_DOWNLOAD_BASE may only name this machine, and is for tests"
  protocols="=http,https"
fi

# Everything is downloaded and checked in a directory of its own, and removed again if
# anything is wrong.
temp="${TMPDIR:-/tmp}"
dir="$(mktemp -d "${temp%/}/ballast.XXXXXX")"
done_well=""
cleanup() {
  [ -n "$done_well" ] || rm -rf "$dir"
}
trap cleanup EXIT

# download fetches $1 into $2, at most $3 bytes. Redirects are followed, a few, and only
# within the protocols allowed for the first request: a release's files are served from
# another host after a redirect, and a redirect to plain http is refused.
download() {
  curl --fail --silent --show-error --location \
    --proto "$protocols" --proto-redir "$protocols" --max-redirs 5 \
    --connect-timeout 20 --max-time 300 --max-filesize "$3" \
    --output "$2" "$1" || die "could not download a file of the release ${version}: is there such a release?"
  [ "$(wc -c < "$2")" -le "$3" ] || die "a file of the release is larger than any of them should be"
}

download "${base%/}/${version}/${file}" "$dir/$file" 100000000
download "${base%/}/${version}/checksums.txt" "$dir/checksums.txt" 1000000

# sum_of prints the SHA-256 of a file, with the tool the runner has.
sum_of() {
  local line
  if command -v sha256sum >/dev/null 2>&1; then
    line="$(sha256sum "$1")"
  else
    line="$(shasum -a 256 "$1")"
  fi
  echo "${line%% *}"
}

actual="$(sum_of "$dir/$file")"
[[ "$actual" =~ ^[0-9a-f]{64}$ ]] || die "could not compute the sum of the download"

# The sums are read here, line by line. There must be exactly one line for the file, in
# the format every checksum tool writes, and it must be the sum of what was downloaded.
# Lines for other files are not this script's business.
found=0
matching=0
while IFS= read -r line || [ -n "$line" ]; do
  line="${line%$'\r'}"
  if [[ "$line" =~ ^([0-9a-f]{64})\ \ (.+)$ ]] && [ "${BASH_REMATCH[2]}" = "$file" ]; then
    found=$((found + 1))
    [ "${BASH_REMATCH[1]}" = "$actual" ] && matching=$((matching + 1))
  fi
done < "$dir/checksums.txt"
[ "$found" -eq 1 ] || die "the sums of the release do not have exactly one line for the file this runner needs"
[ "$matching" -eq 1 ] || die "the download is not the one the release lists: its sum is not the one in checksums.txt"

bin="$dir/ballast${extension}"
mv "$dir/$file" "$bin"
chmod 700 "$bin"
rm -f "$dir/checksums.txt"

# It runs, and it is the version that was asked for. The line is "ballast v1.2.3 (...)".
said="$("$bin" version 2>/dev/null)" || die "the command that was downloaded does not run on this runner"
case "$said" in
  "ballast ${version} ("*) ;;
  *) die "the command that was downloaded is not the version that was asked for" ;;
esac

done_well=1
echo "$bin"
echo "ballast-gitlab: installed ballast ${version} for ${goos}/${goarch}" >&2

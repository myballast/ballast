#!/usr/bin/env bash
#
# Builds the ballast command line tool for every platform a release ships, and the
# sums that go with it.
#
#   ops/release/build.sh VERSION [OUTPUT_DIR]     build into OUTPUT_DIR (default: dist)
#   ops/release/build.sh --check-version VERSION  exit 0 if VERSION is one, 1 if not
#   ops/release/build.sh --list-targets           print the platforms of a release
#
# The release workflow runs this and nothing else to produce what it publishes, and
# `make release-cli` runs it for a trial on a developer's machine: what is tested
# here is what a tag does.
#
# Four things are true of what it makes, and a test holds each in place:
#
#   * the same source gives the same bytes. Every binary is built twice, the second
#     time with an empty build cache, and the script stops if the two differ; the
#     flags below are what makes them equal, from any directory and on any machine
#     with the same Go toolchain. A checksum then says which source this is, and says
#     nothing about when or where it was built.
#   * nothing that is not a version is built, and nothing that was typed is ever run:
#     the version and the platforms are checked against a closed list before they
#     reach a command.
#   * an output that is not empty is never written into.
#   * the sums are checked at the end by verify.sh, which publishing runs again, so a
#     release that does not verify is not published.
#
# TARGETS overrides the platforms, from the six that exist, and is for tests.
#
# Written for the bash that macOS ships (3.2) as much as for the one on a runner.

set -euo pipefail
# A name that holds a * is a name, and not a list of files.
set -f
export LC_ALL=C

PACKAGE="github.com/youssefmahersi/ballast/internal/cli"
ALL_TARGETS="linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64"

die() {
  echo "release: $*" >&2
  exit 1
}

# valid_version says whether $1 is a SemVer version with a leading v: v1.2.3, or
# v1.2.3-rc.1. Build metadata (+) is not accepted, nor is anything the binary would
# not take as its own version (internal/cli keeps at most 64 characters of letters,
# digits and . - + _ and calls anything else a development build).
#
# The characters are checked first, on their own, so that nothing that is not part of
# a version is ever handed to the pattern; the pattern is semver.org's.
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

# sums writes the checksums of the files it is given, in the format every checksum
# tool reads: "<64 hex>  <name>".
sums() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$@"
  else
    shasum -a 256 "$@"
  fi
}

case "${1:-}" in
  --list-targets)
    for target in $ALL_TARGETS; do echo "$target"; done
    exit 0
    ;;
  --check-version)
    valid_version "${2:-}" || die "that is not a version: write it like v1.2.3 or v1.2.3-rc.1, at most 64 characters"
    exit 0
    ;;
esac

version="${1:-}"
valid_version "$version" || die "that is not a version: write it like v1.2.3 or v1.2.3-rc.1, at most 64 characters"

targets="${TARGETS:-$ALL_TARGETS}"
[ -n "$targets" ] || die "there is no platform to build for"
for target in $targets; do
  case " $ALL_TARGETS " in
    *" $target "*) ;;
    *) die "that is not a platform of a release: the platforms are $ALL_TARGETS" ;;
  esac
done

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

# Never into what is there: whoever runs a release again by mistake does not find the
# first one gone, and two builds never mix into one output.
output="${2:-dist}"
if [ -e "$output" ]; then
  [ -d "$output" ] || die "the output is not a directory"
  [ -z "$(ls -A "$output")" ] || die "the output is not empty, and a release is never written over what is there"
fi
mkdir -p "$output"
output="$(cd "$output" && pwd)"

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT

# build compiles the tool for one platform into $3, with a build cache of its own.
#
# What decides the bytes is written here and nowhere else: no cgo, the paths of the
# machine trimmed out, no stamp of the repository's state, no symbol table, no
# debugging information, and the version. The variables of the environment that would
# change a build — somebody's GOFLAGS, an experiment, a microarchitecture level, a
# workspace — are removed first, so that the runner's own settings cannot.
build() {
  (
    cd "$root"
    env -u GOFLAGS -u GOEXPERIMENT -u GOAMD64 -u GOARM64 -u GOWORK -u GOARM -u GO386 \
      CGO_ENABLED=0 GOOS="$1" GOARCH="$2" GOAMD64=v1 GOCACHE="$4" \
      go build -trimpath -buildvcs=false \
        -ldflags "-s -w -X ${PACKAGE}.Version=${version}" \
        -o "$3" ./cmd/ballast
  )
}

names=""
for target in $targets; do
  goos="${target%/*}"
  goarch="${target#*/}"

  name="ballast_${goos}_${goarch}"
  [ "$goos" = "windows" ] && name="${name}.exe"

  build "$goos" "$goarch" "$output/$name" "$(go env GOCACHE)"

  # The second build is the proof. A cache that is empty cannot hand back what the
  # first build compiled, so what is compared is two builds of the same source.
  mkdir -p "$scratch/$target"
  cache="$scratch/cache-${goos}-${goarch}"
  build "$goos" "$goarch" "$scratch/$target/$name" "$cache"
  rm -rf "$cache"

  if ! cmp -s "$output/$name" "$scratch/$target/$name"; then
    rm -rf "$output"
    die "the build of $target is not reproducible: two builds of the same source differ"
  fi

  names="$names $name"
  echo "release: built $name"
done

# The sums, in the order of the names, and then verified by the script that publishing
# uses too: it reads them itself and does not take a checksum tool's word for it. A
# release that does not verify is not one.
(
  cd "$output"
  # shellcheck disable=SC2086
  sums $(printf '%s\n' $names | sort) >checksums.txt
)
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
bash "$here/verify.sh" "$output" || {
  rm -rf "$output"
  die "the sums do not verify"
}

echo "release: $version is in $output, with checksums.txt"

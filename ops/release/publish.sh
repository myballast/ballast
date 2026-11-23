#!/usr/bin/env bash
#
# Publishes a release of the ballast command line tool on GitHub, from what build.sh
# made.
#
#   ops/release/publish.sh TAG [DIST_DIR]      DIST_DIR defaults to dist
#
# GH_TOKEN is the credential gh reads, from the environment and from nowhere else: it is
# never an argument, never printed, and the script never turns tracing on.
#
# Publishing is the one step that cannot be taken back: a published release is read by
# pipelines as soon as it exists, and the tag it sits on is then fixed. So:
#
#   * everything is checked before GitHub is asked anything: the tag is a version, the
#     directory holds exactly the six binaries and checksums.txt of a release, and the
#     sums verify;
#   * a release that exists, or whose existence cannot be told, is never touched;
#   * it is made as a draft with all its files, the files are counted, and only then is
#     it published, so that nobody reads a release with a binary missing;
#   * when something goes wrong the draft this run made is deleted, and nothing else is:
#     a release that is published is never deleted by this script, and neither is the
#     tag, which the maintainer pushed;
#   * no file is ever allowed to overwrite another (there is no --clobber).

set -euo pipefail
# A name that holds a * is a name, and not a list of files.
set -f
export LC_ALL=C
export GH_PROMPT_DISABLED=1

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

die() {
  echo "release: $*" >&2
  exit 1
}

tag="${1:-}"
dist="${2:-dist}"

# One definition of a version, and it is build.sh's.
bash "$here/build.sh" --check-version "$tag" || exit 1

[ -n "${GH_TOKEN:-}" ] || die "GH_TOKEN is not set: gh reads its credential from the environment"
[ -d "$dist" ] || die "the directory to publish is not there"

# The files of a release, named by the script that builds them.
expected="checksums.txt"
for target in $(bash "$here/build.sh" --list-targets); do
  goos="${target%/*}"
  goarch="${target#*/}"
  name="ballast_${goos}_${goarch}"
  [ "$goos" = "windows" ] && name="${name}.exe"
  expected="$expected $name"
done

# Exactly those, no more and no fewer: a file nobody asked for would be published with
# the rest, and a missing one would be a platform nobody can download.
actual="$(ls -A "$dist" | sort | tr '\n' ' ')"
wanted="$(printf '%s\n' $expected | sort | tr '\n' ' ')"
[ "$actual" = "$wanted" ] || die "the directory does not hold exactly the files of a release: $wanted"
for name in $expected; do
  [ -f "$dist/$name" ] || die "$name is not a file"
done
count="$(printf '%s\n' $expected | wc -l | tr -d ' ')"

# The sums are verified again here, by the same script that checked them when they were
# made: what was built is not what is trusted, what is about to be published is.
bash "$here/verify.sh" "$dist" || die "the sums do not verify the files that are about to be published"

# A tag with a hyphen is a pre-release, by SemVer's own definition.
prerelease=""
case "$tag" in
  *-*) prerelease="--prerelease" ;;
esac

# The notes: how to verify what is downloaded, and then what GitHub generates from the
# changes. The tag and the repository are put in by this script, and both are checked.
repo="${GITHUB_REPOSITORY:-myballast/ballast}"
case "$repo" in
  */*/* | *[!A-Za-z0-9._/-]* | "" | /* | */) die "that is not a repository" ;;
esac
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
template="$(cat "$here/notes.md")"
template="${template//@TAG@/$tag}"
template="${template//@REPO@/$repo}"
printf '%s\n' "$template" >"$scratch/notes.md"

# Is there a release for this tag already? Only "not found" lets this go on: a gh that
# cannot reach GitHub says nothing about whether one exists.
if existing="$(gh release view "$tag" --json tagName 2>&1)"; then
  die "a release for $tag already exists, and a release is never overwritten"
fi
case "$existing" in
  *"release not found"*) ;;
  *) die "could not tell whether a release for $tag exists: $(printf '%s' "$existing" | head -n 1 | cut -c 1-200)" ;;
esac

# What the run made, and may take down if it does not go through.
remove_draft() {
  if [ "$(gh release view "$tag" --json isDraft --jq .isDraft 2>/dev/null || true)" = "true" ]; then
    gh release delete "$tag" --yes >/dev/null 2>&1 || true
    echo "release: the draft of $tag was removed" >&2
  fi
}

files=()
for name in $expected; do
  files+=("$dist/$name")
done

create=(release create "$tag" "${files[@]}" --draft --verify-tag --title "$tag" --notes-file "$scratch/notes.md" --generate-notes)
if [ -n "$prerelease" ]; then
  create+=("$prerelease")
fi

if ! gh "${create[@]}"; then
  remove_draft
  die "the release could not be created"
fi

# Looked at before it is published: every file is there.
uploaded="$(gh release view "$tag" --json assets --jq '.assets | length' 2>/dev/null || echo 0)"
if [ "$uploaded" != "$count" ]; then
  remove_draft
  die "the draft holds $uploaded files and a release has $count: it was not published"
fi

gh release edit "$tag" --draft=false
echo "release: $tag is published, with $count files"

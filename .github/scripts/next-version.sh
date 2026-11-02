#!/usr/bin/env bash
# Prints the next SemVer tag (e.g. v1.3.0) derived from the Conventional
# Commits since the latest v* tag reachable from HEAD, or nothing if there
# are no new commits. The first release is v1.0.0.
#   feat!: / fix(scope)!: / "BREAKING CHANGE:" in a body  -> major
#   feat: / feat(scope):                                  -> minor
#   anything else                                         -> patch
set -euo pipefail

last=$(git describe --tags --abbrev=0 --match 'v[0-9]*.[0-9]*.[0-9]*' 2>/dev/null || true)
if [ -z "$last" ]; then
  echo v1.0.0
  exit 0
fi

range="$last..HEAD"
if [ -z "$(git rev-list "$range")" ]; then
  exit 0
fi

IFS=. read -r major minor patch <<< "${last#v}"
subjects=$(git log --format=%s "$range")

if grep -qE '^[a-zA-Z]+(\([^)]*\))?!:' <<< "$subjects" \
  || git log --format=%B "$range" | grep -qE '^BREAKING[ -]CHANGE:'; then
  echo "v$((major + 1)).0.0"
elif grep -qE '^feat(\([^)]*\))?:' <<< "$subjects"; then
  echo "v$major.$((minor + 1)).0"
else
  echo "v$major.$minor.$((patch + 1))"
fi

#!/usr/bin/env bash
set -Eeuo pipefail
: "${TAG_NAME:?}" "${RELEASE_TITLE:?}" "${GITHUB_SHA:?}"
git config user.name 'github-actions[bot]'
git config user.email '41898282+github-actions[bot]@users.noreply.github.com'
if git rev-parse --verify "refs/tags/$TAG_NAME" >/dev/null 2>&1; then
  test "$(git rev-parse "refs/tags/$TAG_NAME^{commit}")" = "$GITHUB_SHA"
else
  git tag -a "$TAG_NAME" "$GITHUB_SHA" -m "$RELEASE_TITLE"
  git push origin "refs/tags/$TAG_NAME"
fi
if gh release view "$TAG_NAME" --repo "$GITHUB_REPOSITORY" >/dev/null 2>&1; then
  gh release edit "$TAG_NAME" --repo "$GITHUB_REPOSITORY" --draft=false --title "$RELEASE_TITLE"
else
  notes=()
  if test -f RELEASE_NOTES.md; then notes+=(--notes-file RELEASE_NOTES.md); fi
  gh release create "$TAG_NAME" --repo "$GITHUB_REPOSITORY" --verify-tag \
    --title "$RELEASE_TITLE" --generate-notes "${notes[@]}"
fi

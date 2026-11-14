#!/usr/bin/env bash
set -euo pipefail

: "${SOURCE_URL:?SOURCE_URL is required}"
: "${MIRROR:?MIRROR is required}"
: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}"

git fetch origin --prune
git fetch "${SOURCE_URL}" main
source_sha="$(git rev-parse FETCH_HEAD)"
base_ref="origin/${MIRROR}"
base_sha="$(git rev-parse "${base_ref}")"

if [ "${source_sha}" = "${base_sha}" ]; then
  echo "[i] ${MIRROR} is already at ${source_sha}."
  exit 0
fi

if git merge-base --is-ancestor "${source_sha}" "${base_sha}"; then
  echo "[i] ${MIRROR} already contains ${source_sha}; nothing to sync."
  exit 0
fi

if [ "${DRY_RUN:-false}" = "true" ]; then
  echo "[dry-run] ${MIRROR} would merge ${source_sha} (current ${base_sha})."
  exit 0
fi

branch="sync/${MIRROR}-${source_sha:0:12}"
git switch -C "${branch}" "${base_ref}"
if ! git merge --no-ff "${source_sha}" -m "merge: ${MIRROR} upstream ${source_sha:0:12}"; then
  gh issue create \
    --repo "${GITHUB_REPOSITORY}" \
    --title "sync conflict: ${MIRROR} ${source_sha:0:12}" \
    --body "Automatic mirror sync failed and needs a manual, no-force resolution.

- source: ${SOURCE_URL} @ ${source_sha}
- mirror: ${MIRROR} @ ${base_sha}"
  exit 1
fi

git push -u origin "${branch}"
gh pr create \
  --repo "${GITHUB_REPOSITORY}" \
  --base "${MIRROR}" \
  --head "${branch}" \
  --title "sync: ${MIRROR} upstream ${source_sha:0:12}" \
  --body "$(printf "Sync \`%s\` from \`%s\`.\n\n- source SHA: \`%s\`\n- previous mirror: \`%s\`\n- no force; review and merge only after CI is green." "${MIRROR}" "${SOURCE_URL}" "${source_sha}" "${base_sha}")"

#!/usr/bin/env bash
set -euo pipefail

: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}"

git fetch origin --prune
git fetch origin main plus --tags

if git merge-base --is-ancestor origin/main HEAD && git merge-base --is-ancestor origin/plus HEAD; then
  echo "[i] dc-use already contains both mirrors."
  exit 0
fi

upstream_sha="$(git rev-parse origin/main)"
plus_sha="$(git rev-parse origin/plus)"
upstream_tag="$(git describe --tags --abbrev=0 "${upstream_sha}" 2>/dev/null || git rev-parse --short "${upstream_sha}")"
plus_tag="$(git describe --tags --abbrev=0 "${plus_sha}" 2>/dev/null || git rev-parse --short "${plus_sha}")"

if [ "${DRY_RUN:-false}" = "true" ]; then
  echo "[dry-run] dc-use is missing origin/main (${upstream_sha}) or origin/plus (${plus_sha})."
  echo "[dry-run] Would open a PR with ${upstream_tag} + ${plus_tag}."
  exit 0
fi

branch="sync/dc-use-$(date -u +%Y%m%d%H%M)"
git switch -C "${branch}" dc-use

if ! git merge --no-ff origin/main -m "merge: upstream main ${upstream_sha:0:12} (${upstream_tag})"; then
  git merge --abort
  gh issue create \
    --repo "${GITHUB_REPOSITORY}" \
    --title "integration conflict: main ${upstream_sha:0:12}" \
    --body "dc-use could not merge origin/main automatically. Resolve manually without force."
  exit 1
fi

if ! git merge --no-ff origin/plus -m "merge: plus ${plus_sha:0:12} (${plus_tag})"; then
  git merge --abort
  gh issue create \
    --repo "${GITHUB_REPOSITORY}" \
    --title "integration conflict: plus ${plus_sha:0:12}" \
    --body "dc-use could not merge origin/plus automatically. Resolve manually without force."
  exit 1
fi

cat > .cpamc-fork-upstream.env <<EOF
UPSTREAM_TAG=${upstream_tag}
UPSTREAM_COMMIT=${upstream_sha}
PLUS_TAG=${plus_tag}
PLUS_COMMIT=${plus_sha}
EOF
git add .cpamc-fork-upstream.env
git commit -m "chore: record upstream ${upstream_tag} and plus ${plus_tag}"

git push -u origin "${branch}"
gh pr create \
  --repo "${GITHUB_REPOSITORY}" \
  --base dc-use \
  --head "${branch}" \
  --title "sync: integrate ${upstream_tag} and plus ${plus_tag}" \
  --body "$(printf "Integrate the mirrors into \`dc-use\`.\n\n- official: \`%s\` (\`%s\`)\n- plus: \`%s\` (\`%s\`)\n- no force; CI must be green before merge." "${upstream_tag}" "${upstream_sha}" "${plus_tag}" "${plus_sha}")"

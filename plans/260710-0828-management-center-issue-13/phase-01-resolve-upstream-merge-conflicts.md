---
phase: 1
title: "Resolve upstream merge conflicts"
status: pending
effort: "4-6 hours"
priority: P1
dependencies: []
---

# Phase 1: Resolve upstream merge conflicts

## Overview

Create the isolated worktree, merge pinned upstream `v1.17.14`, and produce a clean, narrowly resolved branch. This phase owns every tracked-file edit.

## Context Links

- Issue: https://github.com/kaitranntt/Cli-Proxy-API-Management-Center/issues/13
- Latest blocked run: https://github.com/kaitranntt/Cli-Proxy-API-Management-Center/actions/runs/29078221889
- Prior manual sync pattern: https://github.com/kaitranntt/Cli-Proxy-API-Management-Center/pull/12
- Repository contracts: `README.md`, `.github/workflows/upstream-sync.yml`, `.github/workflows/sync-release-tag.yml`

## Requirements

- Functional: fork tree includes upstream `fd22c148`; Qoder, Usage Statistics, and reverse-proxy subpath behavior survive.
- Functional: fork sync marker records exact upstream tag/SHA.
- Non-functional: minimal additive fork delta on top of upstream; no unrelated formatting/refactor churn.
- Non-functional: clean merge graph, no unmerged entries, no secrets/generated output.

## Worktree and Base

Use an isolated path such as:

```bash
cd /Users/kaitran/CloudPersonal/external/Cli-Proxy-API-Management-Center
git fetch origin
git fetch upstream main
git worktree add /Users/kaitran/CloudPersonal/worktrees/cpamc/issue-13-upstream-sync \
  -b kai/chore/13-upstream-sync-v11714 origin/main
cd /Users/kaitran/CloudPersonal/worktrees/cpamc/issue-13-upstream-sync
```

Preflight invariants:

```bash
test "$(git rev-parse origin/main)" = "62634dae39e96b1c49c771206fc085fadfd8005f"
test "$(git ls-remote --tags --refs upstream refs/tags/v1.17.14 | awk '{print $1}')" = \
  "fd22c148286078410f299805ac41b21f29318f24"
git status --short --branch
```

Do not fetch upstream tags into the fork namespace; prior fork maintenance explicitly avoids upstream/fork tag collisions. If `origin/main` changed, stop and re-scout the new fork delta before implementation. If upstream advanced beyond `v1.17.14`, keep this plan pinned to `fd22c148`; later upstream work is separate.

## Fork-vs-Upstream Conflict Policy

1. Use upstream `fd22c148` as the base content for every conflict.
2. Reapply only documented fork contracts, smallest possible hunks.
3. `src/pages/DashboardPage.tsx`: take upstream wholesale. Fork delta since merge base is whitespace-only; #9 behavior was superseded by PR #12.
4. Qoder conflicts: retain upstream xAI/provider structure, then add Qoder as a peer in OAuth provider unions, `is_webui` handling, OAuth UI, quota config/rendering/styles, auth-file/store/type support, and translations.
5. Usage conflicts: retain upstream icon/locale cleanup, then add only referenced Usage route/nav/icon/translation keys. Preserve existing Usage page/API/type/util files.
6. Locale conflicts: start from each upstream JSON file; add only live Qoder and Usage keys. Validate JSON and key references; never keep orphaned upstream-removed keys.
7. Preserve `src/utils/connection.ts` subpath detection after its clean merge. Upstream still lacks this fix.
8. Preserve fork workflows, fork README sections, and release behavior. Update, do not preserve, the stale sync marker.
9. Prefer upstream for xAI, plugin, provider, quota refactors, and all unrelated functionality.
10. Never use repository-wide `git checkout --ours` or `--theirs`.

## Related Code Files

Exclusive manual conflict edits:

- Modify: `/Users/kaitran/CloudPersonal/worktrees/cpamc/issue-13-upstream-sync/src/components/quota/quotaConfigs.ts`
- Modify: `/Users/kaitran/CloudPersonal/worktrees/cpamc/issue-13-upstream-sync/src/components/ui/icons.tsx`
- Modify: `/Users/kaitran/CloudPersonal/worktrees/cpamc/issue-13-upstream-sync/src/i18n/locales/en.json`
- Modify: `/Users/kaitran/CloudPersonal/worktrees/cpamc/issue-13-upstream-sync/src/i18n/locales/ru.json`
- Modify: `/Users/kaitran/CloudPersonal/worktrees/cpamc/issue-13-upstream-sync/src/i18n/locales/zh-CN.json`
- Modify: `/Users/kaitran/CloudPersonal/worktrees/cpamc/issue-13-upstream-sync/src/i18n/locales/zh-TW.json`
- Modify: `/Users/kaitran/CloudPersonal/worktrees/cpamc/issue-13-upstream-sync/src/pages/DashboardPage.tsx`
- Modify: `/Users/kaitran/CloudPersonal/worktrees/cpamc/issue-13-upstream-sync/src/pages/OAuthPage.tsx`
- Modify: `/Users/kaitran/CloudPersonal/worktrees/cpamc/issue-13-upstream-sync/src/pages/QuotaPage.module.scss`
- Modify: `/Users/kaitran/CloudPersonal/worktrees/cpamc/issue-13-upstream-sync/src/services/api/oauth.ts`
- Modify: `/Users/kaitran/CloudPersonal/worktrees/cpamc/issue-13-upstream-sync/src/types/oauth.ts`
- Modify: `/Users/kaitran/CloudPersonal/worktrees/cpamc/issue-13-upstream-sync/.cpamc-fork-upstream.env`

Audit after automatic merge; edit only if contract repair is required:

- Qoder support: `src/components/quota/index.ts`, auth-file components/constants, `src/pages/QuotaPage.tsx`, `src/stores/useQuotaStore.ts`, auth/quota types, model/quota validators, `src/assets/icons/qoder.svg`.
- Usage support: `src/pages/UsagePage.tsx`, `src/pages/UsagePage.module.scss`, `src/components/layout/MainLayout.tsx`, `src/router/MainRoutes.tsx`, `src/services/api/index.ts`, `src/services/api/usage.ts`, usage types/utils.
- Subpath support: `src/utils/connection.ts`.
- Fork automation/docs: `.github/workflows/*`, `README.md`, `README_CN.md`.

No fork-authored new files or deletions expected. Accept upstream additions/deletions only when they are exactly part of `fd22c148`.

## Implementation Steps

1. Refresh `origin` and upstream refs; create the isolated worktree/branch exactly as specified.
2. Prove fork base and pinned upstream tag/SHA. Stop on mismatch.
3. Run `git merge --no-commit --no-ff fd22c148286078410f299805ac41b21f29318f24`.
4. Capture `git diff --name-only --diff-filter=U`; confirm it matches or is explainably narrower than the latest 11-file report.
5. Resolve each conflict using the policy above. Keep upstream dashboard; add Qoder/Usage deltas to upstream structures.
6. Audit all auto-merged fork-contract files. Confirm subpath detection still uses `window.location.pathname`.
7. Set `.cpamc-fork-upstream.env` exactly:

   ```text
   UPSTREAM_TAG=v1.17.14
   UPSTREAM_COMMIT=fd22c148286078410f299805ac41b21f29318f24
   ```

8. Review the combined diff against both parents:

   ```bash
   git diff --check
   git diff --name-only --diff-filter=U
   git diff --stat origin/main
   git diff --stat fd22c148286078410f299805ac41b21f29318f24
   git status --short
   ```

9. Stage explicit files only. Inspect `git diff --cached --check` and `git diff --cached --stat`.
10. Commit the merge resolution with conventional subject `chore(upstream-sync): merge upstream v1.17.14 and resolve conflicts`.
11. Hand off a clean tracked worktree and commit SHA to Phase 2. Do not push yet.

## Todo List

- [ ] Isolated branch/worktree created from exact `origin/main`.
- [ ] Upstream tag/SHA pin verified.
- [ ] Eleven conflicts resolved under upstream-first policy.
- [ ] Qoder and Usage contracts re-applied narrowly.
- [ ] Subpath and fork automation audited.
- [ ] Sync marker corrected.
- [ ] Merge-resolution commit created; tracked worktree clean.

## Success Criteria

- [ ] `git diff --name-only --diff-filter=U` is empty.
- [ ] `git merge-base --is-ancestor fd22c148286078410f299805ac41b21f29318f24 HEAD` succeeds.
- [ ] `DashboardPage.tsx` matches upstream behavior; #9 counting logic absent.
- [ ] Qoder and Usage entry points/types/locale keys remain reachable.
- [ ] `src/utils/connection.ts` retains pathname-derived directory prefix.
- [ ] `.cpamc-fork-upstream.env` matches the pinned tag and SHA.
- [ ] `git diff --check` passes; no generated files, secrets, or unrelated changes staged.

## Risk Assessment

- High: additive Qoder code may be applied to obsolete upstream abstractions. Mitigation: start from upstream files and map Qoder onto current xAI/Kimi peer patterns.
- High: locale conflict resolution can silently lose keys or retain orphans. Mitigation: upstream-first JSON plus reference checks and full build in Phase 2.
- Medium: cleanly merged contract files can be semantically wrong. Mitigation: explicit audit list; do not trust conflict count alone.
- Medium: upstream ref moves. Mitigation: pin tag and SHA; stop on fork-base change.

## Security Considerations

- Preserve OAuth provider allowlists and `is_webui` semantics; do not broaden arbitrary callback/provider input.
- Never expose management keys, auth-file contents, or tokens in diffs/logs/PR text.
- Preserve upstream authentication hardening and use Qoder as an additive known provider only.

## Next Step

Phase 2 validates this exact commit. It owns no tracked-file changes; failures return to this phase.

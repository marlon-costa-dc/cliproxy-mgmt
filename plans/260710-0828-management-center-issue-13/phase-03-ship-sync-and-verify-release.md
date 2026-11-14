---
phase: 3
title: "Ship sync and verify release"
status: pending
effort: "1-2 hours plus CI"
priority: P1
dependencies: [2]
---

# Phase 3: Ship sync and verify release

## Overview

Push the validated branch, merge it through a `main` PR, then prove release provenance, artifact availability, issue closure, and rollback readiness. This phase owns Git/GitHub/release operations only.

## Requirements

- PR targets `main`; branch remains `kai/chore/13-upstream-sync-v11714`.
- `PR Test Build` succeeds before merge even though `main` is unprotected.
- PR body records upstream tag/SHA, conflict policy, preserved contracts, exact validation, and `Refs #13`.
- Release tag points to merged `main`; `management.html` is non-empty, has the correct embedded version, and matches a local rebuild of the exact tag.
- PR title does not include `upstream-sync`, because scheduled automation closes PRs matching that text and deletes their branches.
- Temporarily disable only the mutating `Upstream Sync` and `Sync Fork Release Tag` workflows before opening the manual PR; preserve their definitions and record their enabled states. Merge uses GitHub merge-commit mode with the Phase 2-tested head SHA; base drift between Phase 2 and merge forces refresh plus a full Phase 2 rerun. Re-enable workflows only after both merge parents are proven, then dispatch release tagging for the verified merge ref.
- No source edits in this phase. Code/check failure returns through Phases 1 and 2.

## Expected Shipping Result

- PR title: `chore: integrate dashboard upstream v1.17.14`.
- Merge target: `kaitranntt/Cli-Proxy-API-Management-Center:main`.
- Expected tag: compute the actual candidate using `sync-release-tag.yml`'s release-base/suffix algorithm after the merge; stop if the `0..20` search is exhausted.
- Release asset: `management.html`, built from the selected tag's commit and embedding that tag as `VERSION`.
- Issue #13 state is not a release gate by itself. Do not rely on its automatic no-delta closure; close it manually only after recording release evidence if automation has not already closed it.

## Related Surfaces

- Git branch/commit and worktree metadata.
- GitHub PR, `PR Test Build`, `Sync Fork Release Tag`, `Build and Release`, `Upstream Sync` runs.
- GitHub release/tag and `management.html` asset.
- GitHub issue #13.
- Modify/Create/Delete tracked files: none.

## Implementation Steps

1. Reconfirm clean validated state, record the Phase 1 base SHA, and prove the base has not moved since Phase 2:

   ```bash
   cd /Users/kaitran/CloudPersonal/worktrees/cpamc/issue-13-upstream-sync
   git status --short --branch
   git rev-parse HEAD
   git rev-parse origin/main
   git merge-base --is-ancestor fd22c148286078410f299805ac41b21f29318f24 HEAD
   test "$(git rev-parse origin/main)" = "<recorded-phase-1-base-sha>"
   ```

2. Record the current enabled state of `Upstream Sync` and `Sync Fork Release Tag`, then temporarily disable both workflows through GitHub Actions before the manual PR exists. This prevents scheduled sync from mutating PR state and prevents an unverified merge from releasing. Do not edit their YAML definitions, and restore each workflow to its prior enabled state in the Phase 3 cleanup path even if the PR fails.

3. Push only the work branch:

   ```bash
   git push -u origin kai/chore/13-upstream-sync-v11714
   ```

4. Create a PR to `main` with title `chore: integrate dashboard upstream v1.17.14`. Body must include:
   - upstream `v1.17.14` / `fd22c148286078410f299805ac41b21f29318f24`;
   - upstream-first resolution policy;
   - preserved Qoder, Usage, subpath, fork automation contracts;
   - upstream dashboard/#9 supersession decision;
   - frozen install, lint, type-check, build, and smoke results;
   - `Refs #13`.
5. Verify live PR metadata, review threads, mergeability, base/head SHAs, branch existence, and `PR Test Build`. Address actionable feedback only through Phase 1 ownership, then rerun Phase 2.
6. Immediately before merge, fetch `origin/main` and reassert it equals `<recorded-phase-1-base-sha>` and the PR head equals the validated SHA. If either changed, restore the two workflows to their prior state, return to Phase 1, re-scout the new delta, create a newly reviewed merge-resolution commit, and rerun all Phase 2 gates. Merge only with GitHub merge-commit mode and `--match-head-commit <validated-head-sha>`; do not force-push or bypass checks.
7. Capture merged PR URL, merge commit SHA, and final `main` SHA. Before re-enabling either workflow, prove the merge graph exactly matches the validated inputs:

   ```bash
   test "$(git rev-parse <merge-sha>^1)" = "<recorded-phase-1-base-sha>"
   test "$(git rev-parse <merge-sha>^2)" = "<validated-head-sha>"
   test "$(git rev-parse origin/main)" = "<merge-sha>"
   ```

   Any failure is an operational stop: leave release tagging disabled, report the unexpected base advance, and create a reviewed corrective PR rather than attempting a release.

8. Restore the two workflows to their recorded enabled state. Dispatch `Sync Fork Release Tag` only after the parent checks pass, with the verified `main`/merge ref or its documented workflow-dispatch input; capture the dispatch URL/run ID before proceeding. Verify:

   ```bash
   git fetch origin main --tags
   git merge-base --is-ancestor fd22c148286078410f299805ac41b21f29318f24 origin/main
   ```

9. Watch the manually dispatched `Sync Fork Release Tag` and its `Build and Release` child run. Inspect their named steps/outputs—not only conclusion—and verify the release-tag selection algorithm chose a tag for the exact merged `main` SHA.
10. Prove release and artifact:

   ```bash
   gh release view <actual-tag> -R kaitranntt/Cli-Proxy-API-Management-Center \
     --json tagName,targetCommitish,publishedAt,isDraft,isPrerelease,url,assets
   gh release list -R kaitranntt/Cli-Proxy-API-Management-Center --limit 5
   gh release download <actual-tag> -R kaitranntt/Cli-Proxy-API-Management-Center \
     -p management.html -D /tmp/cpamc-release
   test -s /tmp/cpamc-release/management.html
   test "$(git rev-list -n 1 <actual-tag>)" = "<recorded-merged-main-sha>"
   git worktree add /tmp/cpamc-release-source <actual-tag>
   (cd /tmp/cpamc-release-source && VERSION=<actual-tag> bun install --frozen-lockfile && VERSION=<actual-tag> bun run build && mv dist/index.html /tmp/cpamc-release/local-management.html)
   shasum -a 256 /tmp/cpamc-release/management.html /tmp/cpamc-release/local-management.html
   cmp /tmp/cpamc-release/management.html /tmp/cpamc-release/local-management.html
   rg -F "<actual-tag>" /tmp/cpamc-release/management.html
   ```

   Remove `/tmp/cpamc-release-source` after comparison. Stop if the tag SHA differs from merged `main`, the hash comparison differs, or the expected version is absent.
11. Record release proof first. If issue #13 is still open, close it with the merged PR, tag, asset-hash, and workflow evidence; if automation already closed it, append the same proof rather than treating state as proof.
12. Do not dispatch or wait for the mutating Upstream Sync workflow as validation. Instead, fetch `upstream/main` read-only and prove either `fd22c148` remains its HEAD or `git log --oneline fd22c148..upstream/main` contains only a separately recorded later delta.
13. Record release tag, release URL, tag/main/merge SHAs, asset size/checksum, issue state, prior/final workflow states, and workflow URLs.
14. After all proof, remove the isolated worktree and delete the merged work branch locally/remotely. Never delete release tags.

## Todo List

- [ ] Validated branch pushed without rewriting history.
- [ ] PR to `main` created with complete evidence, neutral title, and `Refs #13`.
- [ ] Reviews and `PR Test Build` green; workflow disable/restore evidence and exact merge-parent checks prove unchanged validated base/head merged with merge-commit mode.
- [ ] Merged `main` contains pinned upstream SHA.
- [ ] Release workflow step outputs prove tag selection and publication for merged `main`.
- [ ] `management.html` downloaded, non-empty, version-checked, and byte-identical to an exact-tag local rebuild.
- [ ] Issue #13 closed with evidence.
- [ ] Post-merge upstream-sync state verified.
- [ ] Worktree/branch cleaned only after release proof.

## Success Criteria

- [ ] PR merged to `main`; two-parent merge/head SHAs match the validated branch and Phase 1 base.
- [ ] `PR Test Build`, `Sync Fork Release Tag`, and `Build and Release` are successful.
- [ ] Release tag resolves to merged `main`; release is neither draft nor prerelease and is marked latest by workflow.
- [ ] Release exposes the selected `management.html` asset with non-zero size, expected embedded version, and exact-tag rebuild match.
- [ ] Issue #13 is closed; no open fork PR remains for this work.
- [ ] Cleanup occurs after, not before, release and rollback evidence is captured.

## Rollback Proof

Before cleanup, record:

- Pre-merge fork main: `62634dae39e96b1c49c771206fc085fadfd8005f`.
- Validated branch SHA, PR merge commit, merged `main`, release tag SHA.
- Merge commit first parent and `git show --stat <merge-sha>`.
- Release URL, asset size, and SHA-256.

If production regression appears:

1. Stop further promotion/consumption; preserve the published tag and asset for auditability.
2. Create a new `kai/revert/13-upstream-sync-v11714` branch from current `main`.
3. Revert the merge resolution with the correct mainline parent (`git revert -m 1 <merge-commit>`) or revert focused follow-up commits as applicable; never reset/force-push `main`.
4. Run the full Phase 2 gate on the revert.
5. PR the revert to `main`, explain impact, and reopen/reference #13.
6. Publish a new corrective suffix selected by the same release-tag algorithm. Do not delete or retarget the bad tag.
7. Verify the corrective asset and latest-release pointer exactly as above.

## Risk Assessment

- High: green merge but bad release provenance. Mitigation: tag-to-main SHA, named workflow-step checks, embedded-version check, and exact-tag rebuild comparison.
- High: automation run appears green while merge/gate steps skipped. Mitigation: inspect required named outputs, not only workflow conclusion.
- High: scheduled sync closes a title-matched PR or main advances during review. Mitigation: neutral title, temporary mutating-workflow disable, `--match-head-commit`, and asserted merge parents before manual release dispatch.
- Medium: release suffix race/exhaustion. Mitigation: calculate using workflow logic; record actual tag or stop on exhausted suffixes.

## Security Considerations

- PR/release text contains no credentials, auth-file data, or private infrastructure details.
- Use normal repository permissions; no bypass, force-push, tag rewrite, or direct `main` mutation.

## Unresolved Questions

None.

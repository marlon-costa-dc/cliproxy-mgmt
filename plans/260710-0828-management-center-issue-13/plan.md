---
title: "Resolve Management Center upstream sync issue 13"
description: "Merge the fork through upstream v1.17.14 while preserving Qoder, Usage Statistics, subpath routing, and fork release contracts."
status: pending
priority: P1
issue: 13
branch: "kai/chore/13-upstream-sync-v11714"
tags: [bugfix, frontend, infra, critical]
blockedBy: []
blocks: []
created: "2026-07-10T12:29:32.245Z"
createdBy: "ck:plan"
source: skill
---

# Resolve Management Center upstream sync issue 13

## Overview

Resolve [issue #13](https://github.com/kaitranntt/Cli-Proxy-API-Management-Center/issues/13) by manually integrating pinned upstream `v1.17.14` (`fd22c148286078410f299805ac41b21f29318f24`) into fork `origin/main` (`62634dae39e96b1c49c771206fc085fadfd8005f`). Preserve documented fork-only behavior, pass the repository gates, merge through a PR to `main`, then prove the `management.html` release and issue closure.

## Expected Output

- Isolated worktree branch: `kai/chore/13-upstream-sync-v11714`, created from refreshed `origin/main`.
- One reviewable upstream merge resolution containing upstream `v1.17.14` plus only required fork deltas.
- Sync marker updated to `UPSTREAM_TAG=v1.17.14` and `UPSTREAM_COMMIT=fd22c148286078410f299805ac41b21f29318f24`.
- PR to fork `main` with `Refs #13` (not an auto-close keyword), a title that does not contain `upstream-sync`, successful `PR Test Build`, and recorded local validation.
- Exact release tag selected by the current `sync-release-tag.yml` algorithm at the verified merged `main` SHA; the run must stop if its suffix search is exhausted.
- `management.html` is non-empty, embeds the selected release version, and SHA-256 matches a clean local rebuild of that exact tag with the matching `VERSION`.

## Scope Boundary

In scope:

- Merge pinned upstream `fd22c148` and resolve the 11 conflicts reported by the latest blocked workflow.
- Preserve Qoder OAuth/quota/auth-file integration, Usage Statistics, reverse-proxy subpath detection, fork automation, and maintained-fork documentation.
- Update stale upstream provenance, validate, PR to `main`, merge, and verify release/issue state.

Out of scope:

- New providers, UX redesign, refactors unrelated to conflict resolution, or automation redesign.
- Reintroducing the superseded dashboard OAuth-provider count from PRs #8/#9.
- Fixing upstream issue #328 or opening an upstream PR; preserve the existing fork fix only.
- Updating CloudPersonal root pointers, CLIProxyAPIPlus consumption, public docs repos, or stale non-default branches.
- Force-pushes, direct pushes to `main`, tag deletion/retargeting, or treating issue closure as release proof.

## Non-Negotiable Constraints

- Work only in an isolated worktree rooted from refreshed `origin/main`; never implement in the shared checkout.
- Branch exactly `kai/chore/13-upstream-sync-v11714`; PR base exactly `main` because fork sync and releases run from `main`.
- Merge exactly upstream `fd22c148`/`v1.17.14`; do not silently absorb later upstream commits under this branch name.
- Start conflicts from upstream content, then reapply narrowly proven fork contracts. No blanket `ours`/`theirs` resolution.
- Preserve unrelated user changes and existing fork commit history.
- Never weaken lint, TypeScript, build, release, or authentication behavior to make gates pass.
- Conventional commits only; no AI references, secrets, private account data, `.env*`, or generated `dist/` artifacts committed.
- PR title must not match the scheduled workflow's `in:title upstream-sync` supersession query.
- Require GitHub's merge-commit method. Temporarily disable the mutating Upstream Sync and release-tag workflows before creating the manual PR; immediately before merge, the branch base must equal the recorded Phase 1 base or the branch must be refreshed and Phase 2 rerun. Merge with the tested head SHA, assert both resulting merge parents, then re-enable and manually dispatch release tagging for that verified merge only.

## Touchpoints and Contracts

| Contract | Required result | Primary touchpoints |
|---|---|---|
| Upstream parity | Fork contains `fd22c148`; upstream xAI/provider/plugin changes win unless a listed fork contract requires an additive delta | 11 conflict files plus cleanly merged upstream files |
| Qoder | OAuth card/start flow, auth-file recognition, quota snapshot rendering, styles, types, and four locales remain functional | `src/pages/OAuthPage.tsx`, `src/services/api/oauth.ts`, `src/types/oauth.ts`, quota/auth-file/store/type/locale files |
| Usage Statistics | `/usage` route, sidebar item/icon, API export, totals/trends UI, and `/v0/management/usage` client remain present | `src/pages/UsagePage*`, layout/router/icons, `src/services/api/usage.ts`, usage types/utils, locales |
| Reverse-proxy subpath | Auto-detected API base retains the served directory prefix | `src/utils/connection.ts` |
| Dashboard | Use upstream dashboard and dedicated auth-files tile; do not restore #9 counting behavior | `src/pages/DashboardPage.tsx` |
| Fork release stream | Automation and README fork contract remain; marker identifies actual synced upstream | `.github/workflows/*`, `.cpamc-fork-upstream.env`, `README.md`, `README_CN.md` |
| Artifact | Vite produces `dist/index.html`; release workflow publishes it as `management.html` | `package.json`, `.github/workflows/release.yml` |

## Exclusive Ownership and Order

| Phase | Exclusive owner scope | Dependency |
|---|---|---|
| 1 | Worktree creation, upstream merge, all tracked source/config/docs conflict edits, sync marker, merge-resolution commit | None |
| 2 | Validation execution and evidence only; no tracked-file edits. Any failure returns to Phase 1 owner | Phase 1 complete |
| 3 | PR, checks, merge, release, issue, and rollback proof only; no source edits. Any code failure returns through Phases 1 then 2 | Phase 2 complete |

One actor owns each phase at a time. No parallel edits. Phase handoff requires a clean tracked worktree and the prior phase success criteria.

## Phases

| Phase | Name | Status |
|-------|------|--------|
| 1 | [Resolve upstream merge conflicts](./phase-01-resolve-upstream-merge-conflicts.md) | Pending |
| 2 | [Validate fork contracts and build](./phase-02-validate-fork-contracts-and-build.md) | Pending |
| 3 | [Ship sync and verify release](./phase-03-ship-sync-and-verify-release.md) | Pending |

## Dependencies

- No overlapping unfinished project plan found.
- GitHub and both remotes must be reachable during implementation.
- Required local tools: Git, GitHub CLI, Bun `1.3.14`, Node `24`-compatible runtime.
- Phase order is strict: `1 -> 2 -> 3`.

## Acceptance Criteria

- [ ] Branch/worktree provenance proves base `origin/main` and merge target `fd22c148`.
- [ ] `git diff --name-only --diff-filter=U` returns empty; all 11 reported conflicts are resolved by policy.
- [ ] `git merge-base --is-ancestor fd22c148286078410f299805ac41b21f29318f24 HEAD` succeeds.
- [ ] Qoder, Usage Statistics, subpath routing, fork automation, and README contracts remain; obsolete #9 behavior does not return.
- [ ] `.cpamc-fork-upstream.env` records exact `v1.17.14` tag and SHA.
- [ ] Frozen install, lint, type-check, build, `git diff --check`, and contract smoke checks pass.
- [ ] PR targets `main`, is mergeable, has successful `PR Test Build`, uses merge-commit mode, and references #13 without prematurely closing it.
- [ ] Merged `main` contains the upstream target, a two-parent merge commit, and the exact Phase 2-tested base/head graph.
- [ ] The release tag calculated by `sync-release-tag.yml` resolves to that merged `main`; downloaded `management.html` is non-empty, embeds the selected tag, and matches a local exact-tag rebuild.
- [ ] Issue #13 is closed only after release evidence is recorded (or auto-closed by a no-delta run, without being treated as that evidence); any newer upstream advance is a separate delta.
- [ ] Rollback SHA, merge parent, release tag, artifact checksum, and corrective-release procedure are recorded.

## Red Team Review

### Session - 2026-07-10

**Findings:** 18 (9 accepted, 9 rejected or narrowed). Evidence came from `.github/workflows/upstream-sync.yml`, `sync-release-tag.yml`, `release.yml`, OAuth/auth-file client paths, and the plan's release phases.

| Finding | Disposition | Plan response |
|---|---|---|
| Scheduled sync can close a PR by title and delete its branch | Accept | Require a title without `upstream-sync`; recheck branch/PR before merge. |
| Merge method and base freshness were unspecified | Accept | Require merge-commit plus Phase 1 base equality before PR and immediately before merge; refresh/revalidate on drift. |
| Workflow success can conceal skipped merge/gate steps | Accept | Add explicit per-step output checks. |
| Release-tag expectation/provenance was too weak | Accept | Derive the tag via the workflow algorithm; rebuild the exact tag and compare asset hash/version. |
| Workflow semantics and Qoder/subpath behavior were only structural checks | Accept | Require workflow diff review and mandatory behavioral probes. |
| Issue auto-close could precede release | Accept | Use `Refs #13`; record release proof independently and close only after it. |
| Main could advance in the window between pre-merge check and GitHub merge/release trigger | Accept | Disable the two mutating workflows around the manual merge, use the tested head SHA, assert the recorded base/head as merge parents, then manually release only that verified commit. |
| Qoder/subpath smoke prose was not runnable | Accept | Specify an ignored Vite SSR/local-server harness with concrete root/nested URL and Qoder request assertions. |
| Broad action pinning, global key-storage redesign, and a new auth-file test harness | Reject | Pre-existing, cross-cutting scope unrelated to this merge; retain redacted diff/secret checks and targeted contract review. |

### Whole-Plan Consistency Sweep

- Files reread: `plan.md`, all three phase files.
- Decision deltas checked: PR title, issue linkage, merge method, base freshness, workflow status, tag selection, asset provenance, behavioral contract checks.
- Reconciled stale references: `Closes #13`, fixed `v1.17.14-0` expectation, and release-after-merge assumptions.
- Unresolved contradictions: 0.

## Unresolved Questions

None.

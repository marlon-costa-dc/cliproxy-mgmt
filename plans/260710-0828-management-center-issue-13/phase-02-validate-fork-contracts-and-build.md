---
phase: 2
title: "Validate fork contracts and build"
status: pending
effort: "1-2 hours"
priority: P1
dependencies: [1]
---

# Phase 2: Validate fork contracts and build

## Overview

Validate the Phase 1 commit mechanically and semantically before any push. This phase owns evidence only and must not edit tracked files.

## Requirements

- Functional: all fork public contracts exist in the merged tree and upstream target is an ancestor.
- Quality: frozen install, lint, type-check, and production build pass without suppressed diagnostics.
- Release readiness: build produces a non-empty single-file `dist/index.html` that release automation can rename to `management.html`.
- Ownership: no tracked-file edits. If any check fails, report exact evidence and return to Phase 1.

## Validation Environment

- Worktree: `/Users/kaitran/CloudPersonal/worktrees/cpamc/issue-13-upstream-sync`
- Bun: repository-pinned `1.3.14`
- CI equivalent: Node `24`, `bun install --frozen-lockfile`, lint, build
- Additional local gate: explicit `bun run type-check`

## Contract Matrix

| Contract | Proof |
|---|---|
| Upstream ancestry | `git merge-base --is-ancestor fd22c148286078410f299805ac41b21f29318f24 HEAD` exits 0 |
| Qoder OAuth | `qoder` remains in OAuth service/type unions, Web UI provider list, OAuth page card, and `is_webui=true` request construction |
| Qoder quota/auth files | Qoder config, store/type wiring, icon, styles, validators, and translations compile |
| Usage Statistics | `/usage` route/sidebar/API export/page files and `/v0/management/usage` client remain |
| Subpath routing | representative `/prefix/management.html` and root locations resolve to the corresponding `/prefix/v0/management` and `/v0/management` bases |
| Dashboard | No OAuth auth-file folding into AI Providers count; upstream dashboard compiles |
| Fork automation | fork workflow files receive semantic diff review; expected merge/gate/fast-forward outputs are explicitly checked rather than relying on workflow conclusion |
| Provenance | marker equals `v1.17.14` and `fd22c148286078410f299805ac41b21f29318f24` |

## Related Code Files

- Read-only: every file listed in Phase 1.
- Build inputs: `/Users/kaitran/CloudPersonal/worktrees/cpamc/issue-13-upstream-sync/package.json`, `bun.lock`, TypeScript/Vite/ESLint config.
- Generated, ignored validation output: `node_modules/`, `dist/index.html`.
- Modify/Create/Delete: none.

## Implementation Steps

1. Confirm Phase 1 commit and clean tracked worktree:

   ```bash
   cd /Users/kaitran/CloudPersonal/worktrees/cpamc/issue-13-upstream-sync
   git status --short --branch
   git diff --check origin/main...HEAD
   git diff --name-only --diff-filter=U
   git merge-base --is-ancestor fd22c148286078410f299805ac41b21f29318f24 HEAD
   ```

2. Remove stale build output, then run exact quality gates with fail-fast shell semantics:

   ```bash
   set -euo pipefail
   bun --version
   test "$(bun --version)" = "1.3.14"
   rm -rf dist
   bun install --frozen-lockfile
   bun run lint
   bun run type-check
   bun run build
   test -s dist/index.html
   ```

3. Run structural contract checks and a redacted diff secret review. Generic UI/i18n words such as `api key` or `password` are not evidence of a secret; block only credential-shaped added values, then manually review the changed paths without printing values:

   ```bash
   rg -n "qoder|QODER_CONFIG" src/pages/OAuthPage.tsx src/services/api/oauth.ts \
     src/types/oauth.ts src/components/quota src/features/authFiles src/stores src/types \
     src/utils src/i18n/locales
   rg -n "UsagePage|/usage|usage_statistics|\\./usage" src/components/layout/MainLayout.tsx \
     src/components/ui/icons.tsx src/router/MainRoutes.tsx src/services/api/index.ts \
     src/pages/UsagePage.tsx src/services/api/usage.ts src/i18n/locales
   rg -n "pathname|directory" src/utils/connection.ts
   rg -n "UPSTREAM_TAG=v1.17.14|UPSTREAM_COMMIT=fd22c148" .cpamc-fork-upstream.env
   if git diff --unified=0 origin/main...HEAD -- . ':!bun.lock' | rg -Pni '^\+[^+].{0,160}(authorization:\s*bearer\s+(?!\$|\{|<|redacted)[A-Za-z0-9._=-]{12,}|(?:api[_-]?key|refresh[_-]?token|client[_-]?secret|password)\s*[:=]\s*["\x27][^"\x27]{12,}["\x27])'; then
     exit 1
   fi
   git diff --name-only origin/main...HEAD -- . ':!bun.lock'
   ```

4. Compare every fork workflow file against `origin/main`; enumerate and approve each intended hunk. Verify `upstream-sync.yml` reports `merge.outputs.passed`, `gate.outputs.passed`, and `ffwd.outputs.did_ff` according to the expected path instead of trusting a green workflow conclusion.
5. Inspect the dashboard provider stats diff against upstream; prove no #9 behavior was restored. Inspect all four locale files for valid JSON through the TypeScript/build gate and confirm referenced Qoder/Usage keys exist in every locale.
6. Run mandatory behavior probes without credentials. Create only ignored files under `/tmp/cpamc-contract-probes/`:
   - `probe-subpath.ts` starts Vite with `middlewareMode: true`, sets `globalThis.window.location` before `ssrLoadModule('/src/utils/connection.ts')`, and asserts both `http://probe.invalid/management.html` -> `/v0/management` and `http://probe.invalid/prefix/management.html` -> `/prefix/v0/management` using the module's public base-resolution function.
   - `probe-qoder-oauth.ts` starts a local Node HTTP server that records method, path, and query, loads the API client/OAuth module with the same Vite SSR server, calls its public configuration setter with `apiBase=<server>/prefix` and a synthetic management key, then invokes `oauthApi.startAuth('qoder')`. It must capture exactly `GET /prefix/v0/management/qoder-auth-url?is_webui=true`; the server returns synthetic JSON and no real OAuth URL is opened.
   - The harness does not import the built browser app, use real credentials, or print request headers. Delete `/tmp/cpamc-contract-probes/` after recording only pass/fail assertions. A failed or unavailable probe blocks Phase 3 rather than becoming a passed structural check.
7. Inspect `dist/index.html` size and verify it is the sole release build payload expected before rename.
8. Re-run `git status --short`; generated ignored files must not alter tracked state.
9. Record command results, commit SHA, workflow-diff review, and behavioral-probe evidence for the PR body. Do not amend code in this phase.

## Todo List

- [ ] Ancestry, merge cleanliness, and diff checks pass.
- [ ] Frozen dependency install passes.
- [ ] Lint, type-check, and production build pass.
- [ ] Non-empty `dist/index.html` exists.
- [ ] Qoder, Usage, subpath, dashboard, automation, provenance, and staged-diff secret checks pass.
- [ ] Tracked worktree remains clean.

## Success Criteria

- [ ] Every required command exits 0 with no hidden warnings/errors.
- [ ] Contract matrix has evidence for every row.
- [ ] No unit/spec suite is claimed; repository has none. Lint/type/build plus contract checks are the explicit gate.
- [ ] Required Qoder/subpath probes pass; optional backend smoke is accurately marked passed, failed, or not run.
- [ ] `git status --short` shows no tracked validation changes.
- [ ] Phase 3 receives commit SHA and concise validation evidence.

## Failure Routing

- Source, type, locale, route, or contract failure: Phase 1 owner fixes, creates a focused follow-up commit, then Phase 2 reruns from step 1.
- Tool/network-only failure: retry once after proving source state unchanged; otherwise stop with exact error.
- Never relax ESLint/TypeScript/build config, delete contract checks, or edit files from this phase.

## Risk Assessment

- High: compilation can pass while a fork route/key is missing. Mitigation: structural contract matrix plus optional UI smoke.
- Medium: stale dependencies could mask lockfile drift. Mitigation: frozen install with pinned Bun.
- Low: generated `dist/`/`node_modules/` dirty perception. Mitigation: verify ignored status and never stage them.

## Security Considerations

- Smoke with test credentials only. Never capture management keys or auth-file payloads in screenshots/logs.
- Do not call real provider OAuth endpoints unless the maintainer explicitly supplies a safe test environment.

## Next Step

Proceed to Phase 3 only with all mechanical gates green and no tracked changes.

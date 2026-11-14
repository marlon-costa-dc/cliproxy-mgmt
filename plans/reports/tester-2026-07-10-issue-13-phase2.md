# Phase 2 Validation — 2026-07-10 — Issue 13

## Final result: PASS

Validated worktree `/Users/kaitran/CloudPersonal/worktrees/cpamc/issue-13-upstream-sync` at `032d50554815e0f006d567baf91675d9f290e49c` (`fix(i18n): restore usage statistics translations`). This supersedes the paused pre-fix validation run. All Phase 2 quality, contract, locale, behavioral-probe, and cleanliness gates passed.

No unit/spec suite claimed; this repository's Phase 2 gate is frozen install, lint, type-check, build, contracts, and probes.

## Preflight and provenance

| Command | Result |
|---|---|
| `git status --short --branch` | PASS — `## kai/chore/13-upstream-sync-v11714...origin/main [ahead 58]`; no tracked/untracked entries. |
| `git rev-parse HEAD` / `git log -1 --format='%H%n%s'` | PASS — exact `032d50554815e0f006d567baf91675d9f290e49c`; locale-fix subject above. |
| `git merge-base --is-ancestor 032d50554815e0f006d567baf91675d9f290e49c HEAD` | PASS — exit 0. |
| `git merge-base --is-ancestor fd22c148286078410f299805ac41b21f29318f24 HEAD` | PASS — exit 0. |
| `git diff --check origin/main...HEAD` | PASS — no output. |
| `git diff --name-only --diff-filter=U` | PASS — no output. |
| `rg -n 'UPSTREAM_TAG=v1.17.14|UPSTREAM_COMMIT=fd22c148' .cpamc-fork-upstream.env` | PASS — marker is `v1.17.14` / `fd22c148286078410f299805ac41b21f29318f24`. |

## Exact-Bun quality gate

Global Bun remains `1.3.9`; validation used the disposable parent-provisioned exact binary `/tmp/cpamc-bun-1.3.14/bun-darwin-aarch64/bun`, confirmed by `bun --version` = `1.3.14`.

```text
rm -rf dist
bun install --frozen-lockfile: PASS — Checked 250 installs across 290 packages; no changes
bun run lint: PASS — eslint completed with no diagnostics
bun run type-check: PASS — tsc --noEmit completed
bun run build: PASS — tsc && vite build; 715 modules transformed
test -s dist/index.html: PASS
wc -c dist/index.html: 2556659
find dist -maxdepth 1 -type f -print: dist/index.html only
```

`git check-ignore -q dist/index.html` and `git check-ignore -q node_modules` both passed.

## Fork contracts

| Check | Result |
|---|---|
| Qoder structural `rg` across OAuth, quota, auth-files, stores, types, utils, locales | PASS — service union, `WEBUI_SUPPORTED`, OAuth card, `QODER_CONFIG`, store/type wiring, validators, icon use, and all locales found. |
| Usage structural `rg` across layout, router, API, page, locales | PASS — sidebar, `/usage` route, `UsagePage`, API export, `usageApi.getUsage('/usage')`, and all locale keys found. |
| Subpath resolver `rg` | PASS — `connection.ts` derives page directory from `pathname` before normalizing API base. |
| Refined credential-shaped diff scan | PASS — no added bearer token or quoted credential-shaped value. Manual redacted changed-path review completed; no values printed. |
| Fork workflows vs `origin/main` | PASS — zero changed workflow files/hunks. Current `upstream-sync.yml` gates merge-dependent work on `merge.outputs.passed`, fast-forward on both `merge.outputs.passed` and `gate.outputs.passed`, then closes stale issue only when `ffwd.outputs.did_ff == 'true'`. |
| Dashboard #9 regression | PASS — `DashboardPage.tsx` matches upstream target; provider-key total remains separate from `authFilesCount`. |
| Locale repair | PASS — `032d505` adds 22 lines in each of `en`, `ru`, `zh-CN`, `zh-TW`; each parses as JSON and has nonempty `auth_login.qoder_oauth_title`, `auth_login.qoder_oauth_button`, `nav.usage_statistics`, and `nav_meta.usage_statistics`. |

## Mandatory SSR/local-server probes

Temporary files only: `/tmp/cpamc-contract-probes/probe-subpath.ts` and `probe-qoder-oauth.ts`. Each used Vite `middlewareMode: true` and Vite SSR source loading; the Qoder probe used a local server, synthetic key/base, synthetic JSON, and captured method/path/query only. It did not open real OAuth or log headers.

```text
PASS http://probe.invalid/management.html -> http://probe.invalid/v0/management
PASS http://probe.invalid/prefix/management.html -> http://probe.invalid/prefix/v0/management
PASS GET /prefix/v0/management/qoder-auth-url?is_webui=true
```

## Final cleanliness

- `/tmp/cpamc-contract-probes/`: deleted.
- Final `git status --short --branch`: branch line only; no tracked/untracked worktree entries.
- Final `git diff --check origin/main...HEAD`: no output.
- Final `git diff --name-only --diff-filter=U`: no output.

## Unresolved questions

- None.

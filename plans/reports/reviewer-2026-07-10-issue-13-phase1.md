# Phase 1 independent review — issue 13

Reviewed `bf750230821b522978287dc85b5bb28952c3814e` against fork parent `62634dae39e96b1c49c771206fc085fadfd8005f` and upstream parent `fd22c148286078410f299805ac41b21f29318f24`.

## Verdict

**Fail — 1 high-severity merge regression.** Do not advance to Phase 2 until fixed and re-reviewed.

### High — Usage Statistics lost every live locale key

- Evidence: `src/pages/UsagePage.tsx:75` and `:102-187` invoke the `usage_stats.*` namespace for the error state, page text, cards, trends, and both tables.
- `git show 62634dae:src/i18n/locales/{en,ru,zh-CN,zh-TW}.json` contains the 19-key `usage_stats` object in every locale; HEAD contains no `usage_stats` object in any locale. The parent-to-HEAD locale diff deletes that namespace.
- Impact: `/usage` remains routable, but its user-visible strings resolve to raw i18n keys in all supported languages.
- Fix: restore the live `usage_stats` object in all four locale files from the fork parent, then rerun JSON/key-reference validation and the Phase 2 build gates.

## Accepted contracts verified

| Contract | Evidence | Result |
| --- | --- | --- |
| Merge graph / provenance | `bf750230` has parents `62634dae` and `fd22c148`; `git merge-base --is-ancestor fd22c148 HEAD` succeeded; no unmerged paths | Pass |
| Qoder OAuth / quota / auth files | Built-in card at `src/pages/OAuthPage.tsx:75-112`; safe provider path and `is_webui` at `src/services/api/oauth.ts:11-45`; quota config at `src/components/quota/quotaConfigs.ts:1947-1964`; auth-file wiring at `src/features/authFiles/constants.ts:34-43` and `AuthFileQuotaSection.tsx:36-71` | Pass (static) |
| Usage entry points | Route `src/router/MainRoutes.tsx:30-33`, sidebar mapping `src/components/layout/MainLayout.tsx:52-64`, API/types/util remain present | Fail: locale regression above |
| Reverse-proxy subpath | `src/utils/connection.ts:20-30` derives and retains the served pathname directory | Pass |
| Dashboard #9 supersession | `git diff fd22c148..bf750230 -- src/pages/DashboardPage.tsx` is empty; current page uses dedicated auth-file count at `src/pages/DashboardPage.tsx:50-109` | Pass |
| Fork workflows / marker | Workflows and READMEs are byte-identical to fork parent; marker is exact at `.cpamc-fork-upstream.env:1-2`; sync workflow writes/reads it at `.github/workflows/upstream-sync.yml:181-186` and `sync-release-tag.yml:58-60` | Pass |
| Scope / hygiene | Fork delta over upstream is 41 expected files; `git diff --check` passed; no tracked `dist/` or dotenv file; sensitive-pattern scan of fork delta found 0 matches | Pass |

Phase 2 commands were not run here; this was the requested read-only code/spec review.

## Unresolved questions

None.

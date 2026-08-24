# Quota Optimization Implementation Plan

**Goal:** Make quota spend enforcement match request monitoring cost display, enforce configured default and alias-selected per-key limits, show paused users clearly, and reduce SQLite stalls caused by cleanup and aggregate queries.

**Architecture:** CPA is a minimal enforcement adapter: it should only expose lightweight API-key pause/resume capability required to stop or restore API-key usage, and must not own quota configuration, spend calculation, override selection, or enterprise alias/user display. The management side, including Usage Service and the management center UI, owns the quota contract: global/default limits, alias-selected per-key overrides, spend computation, pause records for operator display, and calls to CPA pause/resume APIs. Use one quota business definition in the management side: service-local calendar day, service-local calendar week starting Monday, successful requests only, and the same token pricing formula as `src/utils/usage.ts`. SQLite performance fixes should remove avoidable full-table scans and avoid running blocking maintenance in the normal cleanup path.

**Tech Stack:** Go/Gin in `D:/C_projects/CLIProxyAPI`, Go `database/sql` with SQLite in `usage-service`, React/TypeScript quota pages, existing Go unit tests and frontend test/build tooling.

**Risk Level:** high

**Based on:**
- 设计文档: `usage-service/.ccg/plan/enterprise-key-management/artifacts/review-result-1.md`
- 决策记录: 当前审查结论：quota 阻断项为费用/窗口口径不一致、API-key override 未执行、SQLite 清理与聚合性能风险。
- CPA 后端分析: `D:/C_projects/CLIProxyAPI/internal/api/handlers/management/quota.go`, `D:/C_projects/CLIProxyAPI/internal/quota/types.go`, `D:/C_projects/CLIProxyAPI/internal/quota/manager.go`, `D:/C_projects/CLIProxyAPI/internal/quota/store.go`, `D:/C_projects/CLIProxyAPI/internal/quota/enforcer.go`。
- 设计修正: CPA 只承担轻量 API-key 暂停/恢复执行接口；quota 配置、用量计算、override、别名展示与暂停管理展示都在管理端实现，避免把重逻辑压到 CPA 运行端。

---

## Spec

- **R1 费用公式一致:** quota enforcement, self-usage, and request monitoring use the same formula for successful requests only: `prompt=max(input_tokens-max(cached_tokens, cache_tokens),0)`, `cache=max(cached_tokens, cache_tokens)`, `completion=output_tokens`, all priced per 1M tokens and converted to cents. Failed requests do not count toward quota spend even if token fields are present.
- **R2 时间窗口一致:** daily quota uses the service local calendar day; weekly quota uses the service local calendar week starting Monday. Resume times must match those windows.
- **R3 默认限额与单独限额生效:** quota config must support one global/default limit that applies to every key, plus per-person/per-key overrides selected by enterprise key binding alias/user display. Operators must not need to type raw API keys or hashes; the UI selects an enterprise key binding display entry while the persisted enforcement value uses the stable API key hash.
- **R4 SQLite 不阻塞正常流量:** cleanup must not run `VACUUM` in the normal daily purge path. Spend queries must use range predicates and matching indexes.
- **R5 Retention 正确推进:** retention cutoff must be recomputed on each cleanup tick.
- **R6 Regression tests:** tests must assert exact cents, cached-token variants, failed-request exclusion, local window boundaries, default/global limits, alias-selected overrides, retention cutoff movement, and cleanup behavior.
- **R7 暂停管理可读性:** paused-key management must show the paused user or enterprise key alias resolved from enterprise key bindings, not only the raw API key hash. Hash may remain as secondary diagnostic text.

---

## File Structure Map

- `D:/C_projects/CLIProxyAPI/internal/api/handlers/management/quota.go` — CPA lightweight pause/resume API surface. Keep any CPA changes limited to the minimum needed for pausing and resuming API-key usage.
- `D:/C_projects/CLIProxyAPI/internal/quota/manager.go` — CPA pause/resume manager. Do not add spend calculation, override selection, enterprise alias lookup, or management reporting logic here.
- `D:/C_projects/CLIProxyAPI/internal/quota/store.go` — CPA pause state store only if required by the lightweight pause/resume execution contract.
- `D:/C_projects/CLIProxyAPI/internal/quota/enforcer.go` — CPA request middleware that blocks paused API keys by hash.
- `usage-service/internal/store/store.go` — Usage Service SQLite schema, indexes, spend-limit config persistence, spend aggregation, enterprise key bindings, event cleanup.
- `usage-service/internal/store/spend_limit_test.go` — store-level quota spend and config tests.
- `usage-service/internal/collector/spend_limit.go` — reads management-side quota config and enforces pauses via CPA pause/resume API.
- `usage-service/internal/collector/collector.go` — ticker that invokes spend-limit enforcement.
- `usage-service/cmd/cpa-manager/main.go` — Usage Service startup and retention cleanup loop.
- `src/utils/usage.ts` — frontend reference cost formula used by monitoring.
- `src/pages/QuotaLimitsPage.tsx` — quota config UI, including default/global limit and alias-selected overrides.
- `src/pages/PausedKeysPage.tsx` — paused-key management UI that must resolve hashes to user/alias display.
- `src/services/api/quotaLimits.ts` — quota config API contract.
- `src/services/api/enterpriseKeys.ts` and `src/types/enterpriseKey.ts` — source of API key hash to user/alias display mapping.

---

## Cross-Repository Contract Findings

1. **CPA should stay a lightweight execution surface.** CPA should expose only the minimum API-key pause/resume capability needed to stop and restore API-key usage. Do not move quota spend calculation, default/override selection, enterprise alias lookup, or reporting aggregation into CPA.
2. **CPA pause APIs are hash-centric by design.** Existing `PostPauseKey` and `PostResumeKey` accept either an 8-character hash or a raw API key, normalize it through `normalizeKeyHash`, and act on the canonical hash. Keep this hash-centric execution contract.
3. **CPA enforcer only needs key hashes.** `D:/C_projects/CLIProxyAPI/internal/quota/enforcer.go` computes `quota.KeyHash(apiKey)` from authenticated requests and blocks paused keys. Runtime enforcement must not depend on enterprise aliases or management-side quota metadata.
4. **Management side owns quota configuration.** Global/default limits, alias-selected per-key overrides, effective-limit selection, and failed-request cost rules belong in the management side. Usage Service should compute spend for successful events only, select the effective limit per API key hash, and call CPA pause/resume APIs with the hash.
5. **Management side owns pause records for display.** If the UI needs a rich paused-management table, the management side should track or enrich pause records using enterprise key bindings. CPA does not need to return user names or aliases.
6. **Global limit maps to management-side default config.** The global batch limit is the management-side default limit for every key. Per-person/per-key exceptions are management-side overrides with `apply_to='api-key'` and `apply_value=<apiKeyHash>` selected via enterprise key binding display.

---

## T1 Define quota spend contract in tests first

**Spec 需求映射:** R1, R2, R6

**Files:**
- Modify: `usage-service/internal/store/spend_limit_test.go`

- [ ] **Step 1: Add exact cents test for base cost formula**

Add a test case that inserts one priced event with input/output/cache tokens and asserts exact `TodayCents` and `WeekCents`. The assertion must prove the conversion from token price to cents, not just that a row exists.

- [ ] **Step 2: Add cached token variant test**

Add a test where `cache_tokens` is larger than `cached_tokens`. Expected spend must use the larger value for both cache cost and prompt-token subtraction.

- [ ] **Step 3: Add failed-request exclusion test**

Add a test where a failed event has token fields populated. Expected spend must exclude that event completely.

- [ ] **Step 4: Add local daily and weekly window tests**

Add tests using timestamps around service-local midnight and Monday week boundary. Expected behavior: daily includes only events in the current local day, weekly includes only events in the current local Monday-start week.

- [ ] **Step 5: Run store tests and confirm new tests fail before implementation**

Verification target: new tests fail for the existing implementation because it uses UTC day, rolling seven-day week, ignores `cache_tokens`, and does not exclude failed requests.

**Verification:** R1 is covered by exact cents, cached-token, and failed-request exclusion assertions; R2 is covered by local day/week boundary assertions.

**Rollback Rule:** If test setup becomes flaky because it depends on wall-clock timing, revise tests to compute event timestamps relative to `time.Local` before changing production code.

---

## T2 Align `QueryKeySpend` formula and windows

**Spec 需求映射:** R1, R2, R4

**Files:**
- Modify: `usage-service/internal/store/store.go`
- Test: `usage-service/internal/store/spend_limit_test.go`

- [ ] **Step 1: Replace date-function filters with timestamp ranges**

Compute daily and weekly start timestamps in Go using `time.Now()` and `time.Local`. Pass range boundaries as SQL parameters instead of using SQLite `date(...)` or `unixepoch(...)` expressions on `timestamp_ms`.

- [ ] **Step 2: Update SQL cost expression**

Use `max(coalesce(ue.cached_tokens,0), coalesce(ue.cache_tokens,0))` as the cache token count. Use that value for both cache cost and prompt-token subtraction.

- [ ] **Step 3: Exclude failed requests from spend**

Add an explicit failed-request exclusion to the spend query. Expected behavior: only successful usage events contribute to daily and weekly quota spend.

- [ ] **Step 4: Run store tests and confirm spend tests pass**

Verification target: exact cents, cache-token variant, failed-request exclusion, daily window, and weekly window tests all pass.

**Verification:** R1 and R2 pass through store tests; R4 is partially covered by SQL using timestamp range predicates that can use an index.

**Rollback Rule:** If local-time SQL creates unstable results, keep local boundary computation in Go and only pass integer millisecond ranges to SQL.

---

## T3 Support default limits and alias-selected per-key overrides in enforcement

**Spec 需求映射:** R3, R6

**Files:**
- Modify: `usage-service/internal/store/store.go`
- Modify: `usage-service/internal/collector/spend_limit.go`
- Test: `usage-service/internal/store/spend_limit_test.go`
- Test: collector spend-limit tests if an existing collector test file covers enforcement; otherwise add focused tests in the existing collector test package.

- [ ] **Step 1: Extend local spend-limit config model**

Add an overrides field to the local Go config model. Contract: default limits apply to every key; each override stores `apply_to`, `apply_value`, `daily_cents`, and `weekly_cents`; only `apply_to='api-key'` participates in enforcement, and `apply_value` is the stable API key hash resolved from the enterprise key binding selected by alias/user display.

- [ ] **Step 2: Load default and overrides from the management-side config source**

Update the spend-limit config loading path so Usage Service reads the management-side `default` and `overrides` contract without requiring CPA to own that config. The default is the global batch limit for every key. Overrides replace the default only for their matching key hash.

- [ ] **Step 3: Select effective limit per key**

Update enforcement logic so each `KeySpend` uses the matching API-key override when `apply_value` equals the key hash; otherwise it uses the default/global limit. A zero daily or weekly limit disables that specific window for the effective config.

- [ ] **Step 4: Add default and override tests**

Cover: default/global limit pauses every matching key; key with lower override pauses earlier than default; key without override uses default; disabled default with enabled override still enforces the override for the matching key.

- [ ] **Step 5: Run collector and store tests**

Verification target: override behavior is proven by tests that observe pause decisions or effective-limit selection.

**Verification:** R3 is covered by default/global-limit tests and per-key override tests; R6 is covered by explicit default and override regression cases.

**Rollback Rule:** If the existing management-side config shape differs from the frontend API type, stop and align the management config contract before changing CPA. Do not add quota config ownership to CPA to bridge the mismatch.

---

## T4 Reduce SQLite aggregate and cleanup stalls

**Spec 需求映射:** R4, R5, R6

**Files:**
- Modify: `usage-service/internal/store/store.go`
- Modify: `usage-service/cmd/cpa-manager/main.go`
- Test: `usage-service/internal/store/spend_limit_test.go` or another existing store test file

- [ ] **Step 1: Add composite spend-query index**

Add an index that supports filtering by API key and timestamp for spend aggregation. Keep existing single-column indexes unless a later benchmark proves they are redundant.

- [ ] **Step 2: Add retention cleanup index if needed**

Confirm the existing timestamp index supports `delete from usage_events where timestamp_ms < ?`. Do not add another equivalent index.

- [ ] **Step 3: Remove automatic `VACUUM` from `PurgeEventsBefore`**

Keep deletion in the retention path. Do not run full `VACUUM` automatically after each purge. If WAL growth remains a concern, use a non-blocking checkpoint strategy that does not monopolize the only DB connection for a long period.

- [ ] **Step 4: Recompute cleanup cutoff each tick**

Move cutoff calculation inside the cleanup loop so every purge uses `time.Now().AddDate(0,0,-RetentionDays)` at execution time.

- [ ] **Step 5: Add cleanup behavior tests**

Cover that old events are purged and newer events remain. For cutoff movement, extract or isolate the cutoff calculation enough that it can be verified without waiting for real time.

- [ ] **Step 6: Run store tests**

Verification target: retention deletes only old rows; cleanup no longer invokes full database vacuum in the normal purge path.

**Verification:** R4 is covered by removal of blocking maintenance and index-backed range query structure; R5 is covered by cutoff movement test.

**Rollback Rule:** If disk reclamation is a hard operational requirement, add a separate explicit maintenance operation rather than putting full `VACUUM` back into the daily cleanup loop.

---

## T5 Fix quota UI default, alias-selected override, and paused-user display flow

**Spec 需求映射:** R3, R6, R7

**Files:**
- Modify: `src/pages/QuotaLimitsPage.tsx`
- Modify: `src/pages/PausedKeysPage.tsx`
- Modify: `src/i18n/locales/zh-CN.json` if labels change
- Test: existing frontend tests if these pages already have coverage; otherwise use build/typecheck verification.

- [ ] **Step 1: Keep global limits in the default fields**

Use the existing default daily/weekly limit fields as the global batch limit for every key. Do not represent global limits as an override row.

- [ ] **Step 2: Remove duplicate global override option**

Ensure the override scope dropdown does not offer duplicate `global` options. If the page keeps an override scope selector, its meaningful editable override mode should be API-key/person-specific because global is already represented by the default fields.

- [ ] **Step 3: Build override options from enterprise key bindings**

Load enterprise key bindings and show the same operator-facing identity used by the enterprise key management panel, such as user name, alias, email, or masked key suffix depending on available fields. The UI must not require operators to type raw API keys or hashes.

- [ ] **Step 4: Persist stable key hash while displaying alias**

When selecting a displayed alias/user option, save the corresponding `apiKeyHash` as `apply_value`. Keep enough display metadata in page state to render the selected alias/user in the table after reload.

- [ ] **Step 5: Resolve paused key hashes to user or alias display**

Update paused-key management so each paused entry resolves `key_hash` through enterprise key bindings and displays the user name or alias as the primary value. Keep the hash visible as secondary diagnostic text or fallback text when no binding exists.

- [ ] **Step 6: Show explicit unresolved-binding state**

If a paused key hash has no enterprise binding, show a clear fallback such as an unresolved/missing binding label plus the hash. Do not make the page look identical to the old hash-only implementation.

- [ ] **Step 7: Run frontend validation**

Verification target: quota page typechecks/builds, default fields clearly represent global limits, override payloads are selected by alias/user display while matching backend key-hash enforcement, and paused-key management shows user/alias as the primary paused subject when bindings exist.

**Verification:** R3 is covered by UI payload matching backend override semantics and by the page making global default limits distinct from alias-selected per-key overrides. R7 is covered by paused-key rows resolving key hashes to user/alias display with hash as secondary fallback.

**Rollback Rule:** If enterprise key bindings are unavailable, the UI must show an explicit empty/dependency state instead of silently falling back to a raw key/hash input.

---

## T6 End-to-end verification and review gate

**Spec 需求映射:** R1, R2, R3, R4, R5, R6, R7

**Files:**
- No planned source edits unless verification exposes a defect.

- [ ] **Step 1: Run backend package tests**

Verification target: store and collector tests pass and include exact spend, windows, overrides, and cleanup cases.

- [ ] **Step 2: Run frontend validation**

Verification target: TypeScript/build checks pass for quota pages, paused-key management, enterprise key binding display mapping, and shared API types.

- [ ] **Step 3: Inspect SQL query shape**

Verification target: `QueryKeySpend` uses millisecond range predicates and does not wrap `timestamp_ms` in SQLite date functions.

- [ ] **Step 4: Inspect cleanup behavior**

Verification target: normal retention cleanup deletes old rows but does not run full `VACUUM` automatically.

- [ ] **Step 5: Independent review**

Ask a reviewer to check only the changed quota/store/collector/UI files against R1-R6.

**Verification:** All requirements have direct test or inspection evidence.

**Rollback Rule:** If backend tests pass but frontend payload contract differs, stop before release and align API contract first.

---

## Spec-to-Verification Traceability

- R1（费用公式一致）→ T1 exact cents, cached-token, and failed-request exclusion tests; T2 formula implementation; T6 SQL/query inspection.
- R2（时间窗口一致）→ T1 local daily/weekly boundary tests; T2 local range implementation; T6 backend tests.
- R3（默认限额与单独限额生效）→ T3 default/global and override persistence/enforcement tests; T5 alias-selected UI payload contract; T6 reviewer gate.
- R4（SQLite 不阻塞正常流量）→ T2 timestamp range predicates; T4 composite index and no automatic VACUUM; T6 cleanup inspection.
- R5（Retention 正确推进）→ T4 cutoff recalculation and cleanup behavior tests.
- R6（Regression tests）→ T1, T3, T4 targeted tests; T6 full verification.
- R7（暂停管理可读性）→ T5 paused-key user/alias resolution; T6 frontend validation and reviewer gate.

---

## Approval Gates

- **Gate A resolved:** Business window is service-local calendar day and service-local Monday-start week.
- **Gate B resolved:** Failed requests do not count toward quota spend.
- **Gate C resolved:** Global/default limits apply to every key; separate per-key overrides must be selected through enterprise key binding alias/user display and resolved to stable API key hashes for enforcement.

---

## Execution Options

1. **Subagent 驱动（推荐）** — T1-T2, T3, T4, T5 可分子任务推进，每个子任务后独立 review。
2. **内联执行** — 在本会话中按 T1 → T6 顺序执行，每个任务完成后给 checkpoint。

计划已完成。请在继续前审查，如需修改请告诉我。

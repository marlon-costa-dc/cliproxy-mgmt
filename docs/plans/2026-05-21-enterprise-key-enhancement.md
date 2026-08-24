# Enterprise Key Enhancement Implementation Plan

> Status update (2026-05-21): Accepted in runtime validation. Enterprise keys page shows email column and add/edit email inputs; usage-report JSON/CSV exports verified with `Authorization: Bearer admin`; re-import resolved empty `apiKey` export rows.

**Goal:** Add email field to enterprise key bindings (full-stack: schema, API, CSV import/export, UI) and expose a `usage-report` endpoint for aggregated API key usage statistics.

**Architecture:** Single-phase with 3 internal checkpoints. Checkpoint 1 adds email to the Go backend (SQLite schema migration, CRUD updates, CSV parser column-header matching). Checkpoint 2 updates the React frontend (types, list column, add/edit modal, CSV export). Checkpoint 3 adds the new `GET /v0/management/enterprise/usage-report` aggregation endpoint with JSON/CSV output.

**Tech Stack:** Go 1.24 (net/http, modernc.org/sqlite), React 19 + TypeScript + Zustand, SQLite

**Risk Level:** medium (schema migration, CSV format backward compatibility, SQL aggregation performance)

**Based on:**
- 设计文档: `.ccg/plan/enterprise-key-enhancement/spec.md`
- 决策记录: `.ccg/plan/enterprise-key-enhancement/decisions.md`
- Roadmap: `.ccg/plan/enterprise-key-enhancement/roadmap.md`

---

## Checkpoint 1 — Backend Email Schema

### T1: Add Email field to EnterpriseKeyBinding struct

**Spec 需求映射:** R1（邮箱字段 — 后端 Schema）

**Files:**
- Modify: `usage-service/internal/store/store.go`（EnterpriseKeyBinding 结构体）

**Steps:**

- [ ] **Step 1: Add Email field**

在 `EnterpriseKeyBinding` 结构体中新增字段，位置在 `DepartmentResolvedBy` 之后：
```go
Email string `json:"email,omitempty"`
```

- [ ] **Step 2: Run Go type check**

Run: `go build ./...`（在 usage-service 目录下）
Expected: 编译通过（新增字段暂时未在 CRUD 中使用，不影响编译）

**Verification:** `EnterpriseKeyBinding` 结构体包含 `Email string` 字段，JSON tag 为 `email`

**Rollback Rule:** 如果编译失败，撤销结构体修改

---

### T2: Add email column migration

**Spec 需求映射:** R1（邮箱字段 — 后端 Schema）— SQLite 迁移幂等

**Files:**
- Modify: `usage-service/internal/store/store.go`（ensureEnterpriseSchema 函数）

**Steps:**

- [ ] **Step 1: Add migration statement**

在 `ensureEnterpriseSchema()` 的 `bindingMigrations` 切片末尾追加：
```go
`alter table enterprise_key_bindings add column email text not null default ''`,
```
该迁移利用已有的 `duplicate column` 容错机制，重复执行不报错。

- [ ] **Step 2: Run Go build**

Run: `go build ./...`
Expected: 编译通过

**Verification:** 启动 usage-service 后 SQLite `enterprise_key_bindings` 表存在 `email` 列（可通过 `pragma table_info(enterprise_key_bindings)` 确认）

**Rollback Rule:** 如果迁移语句导致 init 失败，检查 `duplicate column` 容错逻辑

---

### T3: Update UpsertEnterpriseKeyBindings to handle email

**Spec 需求映射:** R1（所有现有 CRUD 操作支持 email 字段读写）

**Files:**
- Modify: `usage-service/internal/store/store.go`（UpsertEnterpriseKeyBindings 函数）

**Steps:**

- [ ] **Step 1: Update SQL statement**

在 INSERT/UPDATE 语句中增加 `email` 列：
- INSERT 列列表追加 `email`
- VALUES 追加 `?`
- ON CONFLICT DO UPDATE 追加 `email=excluded.email`

- [ ] **Step 2: Pass email value**

在 `stmt.ExecContext` 调用中加入 `strings.TrimSpace(item.Email)` 参数

- [ ] **Step 3: Run Go build**

Run: `go build ./...`
Expected: 编译通过

**Verification:** 通过 API 创建 Key Binding 时传入 email，数据库中 `email` 列正确存储

**Rollback Rule:** 如果 ExecContext 参数数量不匹配，检查 VALUES 和参数顺序

---

### T4: Update LoadEnterpriseKeyBindings to scan email

**Spec 需求映射:** R1（GET key-bindings 返回 email）

**Files:**
- Modify: `usage-service/internal/store/store.go`（LoadEnterpriseKeyBindings 函数）

**Steps:**

- [ ] **Step 1: Update SELECT and Scan**

SELECT 列列表中追加 `email`，Scan 参数列表中追加 `&item.Email`

- [ ] **Step 2: Run Go build**

Run: `go build ./...`
Expected: 编译通过

**Verification:** `GET /v0/management/enterprise/key-bindings` 返回的 JSON 中每条记录包含 `email` 字段

**Rollback Rule:** 如果 Scan 参数数量不匹配，对齐 SELECT 列和 Scan 参数

---

### T5: Update import handler to pass email

**Spec 需求映射:** R3（CSV 导入支持邮箱）

**Files:**
- Modify: `usage-service/internal/httpapi/server.go`（handleImportEnterpriseKeyBindings 函数）

**Steps:**

- [ ] **Step 1: Pass email in toUpsert construction**

在构造 `store.EnterpriseKeyBinding` 时追加 `Email` 字段赋值：
```go
Email: strings.TrimSpace(item.Email),
```

- [ ] **Step 2: Run Go build**

Run: `go build ./...`
Expected: 编译通过（前提：KeyGenPreviewItem 已增加 Email 字段 — 见 T8）

**Verification:** 导入含邮箱列的 CSV 后，数据库中 email 字段正确存储

**Rollback Rule:** 如果 KeyGenPreviewItem 尚无 Email 字段，先完成 T8

---

### T6: Update create key binding handler to accept email

**Spec 需求映射:** R1（PUT key-bindings 创建时可接受 email）

**Files:**
- Modify: `usage-service/internal/httpapi/server.go`（handleCreateEnterpriseKeyBinding 函数）

**Steps:**

- [ ] **Step 1: Add email to request struct**

在 `keyBindingCreateRequest` 结构体中新增：
```go
Email string `json:"email,omitempty"`
```

- [ ] **Step 2: Pass email to store**

在构造 `store.EnterpriseKeyBinding` 时追加 `Email` 字段赋值

- [ ] **Step 3: Run Go build**

Run: `go build ./...`
Expected: 编译通过

**Verification:** `PUT /v0/management/enterprise/key-bindings` 传入 `{"userName":"test","departmentId":"xxx","email":"test@c.com"}`，返回的记录包含 email

**Rollback Rule:** 如果请求结构体未正确解码，检查 JSON tag

---

### T7: Update update key binding handler to accept email

**Spec 需求映射:** R1（PATCH key-bindings 可修改 email）

**Files:**
- Modify: `usage-service/internal/httpapi/server.go`（handleUpdateEnterpriseKeyBinding 函数）

**Steps:**

- [ ] **Step 1: Add email to update request struct**

在更新请求结构体中新增 `Email string` 字段（带 `json:"email,omitempty"` tag）

- [ ] **Step 2: Update the upsert call**

在向 store 传入更新数据时，追加 `Email` 字段

- [ ] **Step 3: Run Go build**

Run: `go build ./...`
Expected: 编译通过

**Verification:** `PATCH /v0/management/enterprise/key-bindings/:apiKey` 传入 `{"email":"new@c.com"}`，返回的记录 email 已更新

**Rollback Rule:** 注意 PATCH 语义——空字符串应被接受并存储为空

---

### T8: Update ParseCSVPreview to detect email column

**Spec 需求映射:** R3（导入按列头名称匹配，支持邮箱/email 变体）

**Files:**
- Modify: `usage-service/internal/usage/keygen.go`（ParseCSVPreview, KeyGenPreviewItem）

**Steps:**

- [ ] **Step 1: Add Email to KeyGenPreviewItem**

```go
Email string `json:"email,omitempty"`
```

- [ ] **Step 2: Add email column detection**

在列头解析循环中增加 email 列匹配：
- `email` / `邮箱` / `e-mail` / `邮件` → 记录 emailIdx

- [ ] **Step 3: Read email value from rows**

在行解析循环中，如果 emailIdx >= 0 且行长度足够，读取 email 值：
```go
email := ""
if emailIdx >= 0 && len(row) > emailIdx {
    email = strings.TrimSpace(row[emailIdx])
}
```
赋值到 `KeyGenPreviewItem.Email`

- [ ] **Step 4: Run Go build + existing tests**

Run: `go build ./... && go test ./internal/usage/ -run TestParseCSV -v`
Expected: 编译通过，已有测试仍通过

**Verification:** 传入含 `邮箱` 列的 CSV，preview 返回的 items 含 email 字段；传入旧格式 CSV（无邮箱列），preview 正常返回且 email 为空

**Rollback Rule:** 如果 header 匹配过于宽松导致误判，收紧匹配规则

---

### T9: Update generate handler to pass email through

**Spec 需求映射:** R3（导入预览含邮箱）

**Files:**
- Modify: `usage-service/internal/httpapi/server.go`（handleGenerateEnterpriseKeyBindings 函数）

**Steps:**

- [ ] **Step 1: Update the generate request struct**

在 `keyBindingGenerateRequest` 结构体中（如果存在）确认 email 已通过 `KeyGenPreviewItem` 传递。此接口调用 `ParseCSVPreview` 后直接返回 items，如果 `KeyGenPreviewItem` 已含 Email 字段则无需额外修改。

- [ ] **Step 2: Verify data pass-through**

Run: `go build ./...`
Expected: 编译通过

**Verification:** POST `/v0/management/enterprise/key-bindings/generate` 传入含邮箱列的 CSV，返回的 preview items 包含 email

**Rollback Rule:** 如果生成接口未自动传递 email，检查 `KeyGenPreviewItem` 序列化

---

### T10: Write backend tests for email schema

**Spec 需求映射:** R1（后端测试覆盖 schema 迁移、email CRUD、新老 CSV 格式导入）

**Files:**
- Create/Modify: `usage-service/internal/store/store_test.go`（store 层测试）
- Create/Modify: `usage-service/internal/httpapi/server_test.go`（API 层测试）
- Create/Modify: `usage-service/internal/usage/keygen_test.go`（CSV 解析测试）

**Steps:**

- [ ] **Step 1: Add email migration test**

测试 `ensureEnterpriseSchema()` 幂等性——连续调用两次不报错，email 列存在

- [ ] **Step 2: Add email CRUD test**

测试创建、读取、更新 Key Binding 时 email 字段正确存取

- [ ] **Step 3: Add CSV parsing test**

测试：
- 新格式 CSV（含 `邮箱` 列）正确解析 email
- 旧格式 CSV（无邮箱列）正常解析，email 为空
- 英文列头 `email` 正确识别

- [ ] **Step 4: Run all tests**

Run: `go test ./...`
Expected: 全部通过

**Verification:** 所有新增测试覆盖 email schema 迁移、CRUD 操作、新老 CSV 格式

**Rollback Rule:** 测试失败则逐条排查，不回滚

---

### T11: Commit Checkpoint 1

**Steps:**

- [ ] **Step 1: Stage and commit**

```bash
git add usage-service/
git commit -m "feat(enterprise-keys): add email field to backend schema, CRUD, and CSV parsing [AIGC]"
```

**Verification:** `git status` 显示 clean，`go test ./...` 在 usage-service 下通过

---

## Checkpoint 2 — Frontend Email UI + Import/Export

### T12: Update EnterpriseKeyBinding type

**Spec 需求映射:** R2（类型定义新增 email）

**Files:**
- Modify: `src/types/enterpriseKey.ts`

**Steps:**

- [ ] **Step 1: Add email field**

```ts
export interface EnterpriseKeyBinding {
  // ... existing fields ...
  email?: string;
}
```

- [ ] **Step 2: Add email to KeyGenPreviewItem**

```ts
export interface KeyGenPreviewItem {
  // ... existing fields ...
  email?: string;
}
```

- [ ] **Step 3: Run type check**

Run: `npm run type-check`
Expected: 类型检查通过（或仅有未使用警告）

**Verification:** TypeScript 编译通过，`EnterpriseKeyBinding` 和 `KeyGenPreviewItem` 含 `email?: string`

**Rollback Rule:** 如果类型检查失败，检查其他引用处的类型兼容性

---

### T13: Update enterpriseKeysApi to pass email

**Spec 需求映射:** R2（API 层传递 email）

**Files:**
- Modify: `src/services/api/enterpriseKeys.ts`

**Steps:**

- [ ] **Step 1: Update createKeyBinding signature**

```ts
createKeyBinding: (data: { userName: string; departmentId: string; apiKey?: string; email?: string }) =>
  apiClient.post<EnterpriseKeyBinding>(`${BASE}/key-bindings`, data),
```

- [ ] **Step 2: Update updateKeyBinding signature**

```ts
updateKeyBinding: (apiKey: string, data: { userName: string; departmentId: string; email?: string }) =>
  apiClient.patch<EnterpriseKeyBinding>(`${BASE}/key-bindings/${encodeURIComponent(apiKey)}`, data),
```

- [ ] **Step 3: Run type check**

Run: `npm run type-check`
Expected: 通过

**Verification:** API 函数签名接受 email 参数

**Rollback Rule:** 保持向后兼容——email 为可选参数

---

### T14: Update EnterpriseKeysPage list to show email column

**Spec 需求映射:** R2（列表新增邮箱列，空值显示 "-"）

**Files:**
- Modify: `src/pages/EnterpriseKeysPage.tsx`（表格列定义）
- Modify: `src/pages/EnterpriseKeysPage.module.scss`（可能需要列宽调整）

**Steps:**

- [ ] **Step 1: Add email column to table**

在表格列定义中，"用户名"列之后新增"邮箱"列：
```tsx
{
  title: t('enterpriseKeys.email'), // or '邮箱'
  dataIndex: 'email',
  key: 'email',
  render: (value: string | undefined) => value || '-',
}
```

- [ ] **Step 2: Run type check + dev server**

Run: `npm run type-check`
Expected: 类型检查通过

**Verification:** 页面表格显示"邮箱"列，有值的行显示邮箱，无值的行显示 "-"

**Rollback Rule:** 如果列布局异常，调整列宽

---

### T15: Update add/edit modal to include email input

**Spec 需求映射:** R2（添加/编辑弹窗增加邮箱输入，可选，不校验格式）

**Files:**
- Modify: `src/pages/EnterpriseKeysPage.tsx`（添加/编辑弹窗表单）

**Steps:**

- [ ] **Step 1: Add email input to create modal**

在"用户名"输入框后新增"邮箱"输入框，`type="text"`，无格式校验，placeholder 如"选填"

- [ ] **Step 2: Add email input to edit modal**

编辑弹窗同样新增邮箱输入框，预填当前值

- [ ] **Step 3: Wire email to API calls**

创建/编辑提交时将 email 值传入 API

- [ ] **Step 4: Run type check**

Run: `npm run type-check`
Expected: 通过

**Verification:** 添加 Key 时可输入邮箱并保存；编辑 Key 时可修改邮箱；列表刷新后显示正确的邮箱值

**Rollback Rule:** 如果表单提交失败，检查 API 参数格式

---

### T16: Update export CSV to include email column

**Spec 需求映射:** R3（导出 CSV 列改为 用户名, 邮箱, API Key, 部门）

**Files:**
- Modify: `src/pages/EnterpriseKeysPage.tsx`（导出函数）

**Steps:**

- [ ] **Step 1: Update CSV header**

```ts
const header = '用户名,邮箱,API Key,部门';
```

- [ ] **Step 2: Update CSV body**

在每行数据中追加 email 字段（用引号包裹，处理空值）：
```ts
[item.userName, item.email || '', item.apiKey || '', departmentName]
```

- [ ] **Step 3: Verify export**

手动验证：选中若干行导出 CSV，用 Excel 打开确认列顺序和内容正确

**Verification:** 导出的 CSV 文件列顺序为 用户名、邮箱、API Key、部门，空邮箱显示为空单元格

**Rollback Rule:** 如果旧脚本依赖旧格式 CSV，提供过渡说明

---

### T17: Update import preview to handle email column

**Spec 需求映射:** R3（导入预览支持邮箱）

**Files:**
- Modify: `src/pages/EnterpriseKeysPage.tsx`（导入预览表格）

**Steps:**

- [ ] **Step 1: Check preview table columns**

确认 `generatePreview` 返回的 items 已包含 email（后端已在 T8 完成），前端预览表格显示 email 列（可选）

- [ ] **Step 2: Run type check**

Run: `npm run type-check`
Expected: 通过

**Verification:** 导入含邮箱列的 CSV 后，预览界面显示邮箱信息

**Rollback Rule:** 如果预览表格不需要显示邮箱（只需确认导入后数据正确），可跳过表格列修改

---

### T18: Update i18n strings

**Spec 需求映射:** R2（i18n 中英文新增相关文案）

**Files:**
- Modify: `src/i18n/locales/zh-CN.json`
- Modify: `src/i18n/locales/en.json`

**Steps:**

- [ ] **Step 1: Add Chinese strings**

```json
"enterpriseKeys": {
  "email": "邮箱",
  "emailPlaceholder": "选填",
  ...
}
```

- [ ] **Step 2: Add English strings**

```json
"enterpriseKeys": {
  "email": "Email",
  "emailPlaceholder": "Optional",
  ...
}
```

- [ ] **Step 3: Run type check**

Run: `npm run type-check`
Expected: 通过

**Verification:** 中英文切换时，邮箱列标题和输入框 placeholder 正确显示

**Rollback Rule:** 如果 i18n key 不匹配，检查 `t()` 调用中的 key 路径

---

### T18a: Display current CPA address in monitoring UI

**Spec 需求映射:** R0（CPA 地址可视化，防止误操作导致数据源不可见）

**Files:**
- Modify: `src/pages/ConfigPage.tsx`（manager 配置区域，或 monitoring 页面 header）

**Steps:**

- [ ] **Step 1: Add CPA address display**

在监控相关页面（monitoring header 或 config manager 区域）显示当前 usage-service 监控的 CPA 地址。
从 `managerConfig.cpaConnection.cpaBaseUrl` 读取，显示格式如 "监控 CPA: http://127.0.0.1:8317"

- [ ] **Step 2: Run type check**

Run: `npm run type-check`
Expected: 通过

**Verification:** 页面可见当前 CPA 地址，切换后显示更新

**Rollback Rule:** 如果读取 managerConfig 失败，显示 "未配置"

---

### T18b: Add CPA address change confirmation

**Spec 需求映射:** R0（CPA 地址变更确认弹窗，防止误操作）

**Files:**
- Modify: `src/pages/ConfigPage.tsx`（handleManagerSave 逻辑）

**Steps:**

- [ ] **Step 1: Add change detection**

在 `handleManagerSave` 中，比较新旧 `cpaBaseUrl`。如果不同，弹出确认对话框：
- 标题："确认切换 CPA 地址"
- 内容："当前监控 CPA 地址为 {oldUrl}，将切换至 {newUrl}。切换后历史数据不会丢失，但新地址的监控数据将从切换时刻开始采集。是否继续？"
- 确认/取消按钮

- [ ] **Step 2: Run type check**

Run: `npm run type-check`
Expected: 通过

**Verification:** 修改 CPA 地址并保存时弹出确认框，点击"取消"不保存，点击"确认"执行切换

**Rollback Rule:** 如果确认框逻辑影响正常保存，加条件判断仅 CPA 地址变化时弹窗

---

### T19: Run frontend tests

**Spec 需求映射:** R2, R3（前端全链路验证）

**Steps:**

- [ ] **Step 1: Run all frontend tests**

Run: `npm run test`
Expected: 全部通过（新增/修改的测试覆盖 email 导入导出、类型定义）

- [ ] **Step 2: Run lint**

Run: `npm run lint`
Expected: 无新增错误

**Verification:** 前端测试套件通过，lint 通过

**Rollback Rule:** 测试失败则逐个修复，不回滚

---

### T20: Commit Checkpoint 2

**Steps:**

- [ ] **Step 1: Stage and commit**

```bash
git add src/
git commit -m "feat(enterprise-keys): add email field to frontend UI, import/export, and CPA switch confirmation [AIGC]"
```

**Verification:** `git status` clean

---

## Checkpoint 3 — usage-report API

### T21: Add AggregateUsageReport store method

**Spec 需求映射:** R4（usage-report 接口）, R5（SQL 聚合性能）

**Files:**
- Modify: `usage-service/internal/store/store.go`（新增方法）

**Steps:**

- [ ] **Step 1: Define result types**

```go
type UsageReportItem struct {
    APIKey            string               `json:"apiKey"`
    UserName          string               `json:"userName"`
    Email             string               `json:"email"`
    DepartmentID      string               `json:"departmentId"`
    DepartmentName    string               `json:"departmentName"`
    TotalTokens       int64                `json:"totalTokens"`
    TotalRequests     int64                `json:"totalRequests"`
    FailedRequests    int64                `json:"failedRequests"`
    CachedTokens      int64                `json:"cachedTokens"`
    TotalCacheTokens  int64                `json:"totalCacheTokens"`
    CacheHitRate      float64              `json:"cacheHitRate"`
    Models            []UsageReportModel   `json:"models"`
}

type UsageReportModel struct {
    Model       string `json:"model"`
    TotalTokens int64  `json:"totalTokens"`
    Requests    int64  `json:"requests"`
}
```

- [ ] **Step 2: Implement AggregateUsageReport method**

逻辑描述：
1. SQL 聚合查询 usage_events 表，GROUP BY api_key_hash, model，WHERE timestamp_ms BETWEEN fromMs AND toMs
2. 仅统计 `failed=0` 或有 token 消耗的事件（`input_tokens > 0 OR output_tokens > 0 OR failed = 1`）
3. 聚合字段：SUM(total_tokens), COUNT(*), SUM(CASE WHEN failed=1 THEN 1 ELSE 0 END), SUM(cached_tokens), SUM(cache_tokens), SUM(CASE WHEN cached_tokens>0 THEN 1 ELSE 0 END)
4. 关联 enterprise_key_bindings（通过 api_key_hash）获取 userName, email, departmentId
5. 关联 enterprise_departments 获取 departmentName
6. 计算 cacheHitRate = cacheHits / totalRequests（总请求数为 0 时返回 0）
7. 按 department_name, user_name 排序

- [ ] **Step 3: Run Go build**

Run: `go build ./...`
Expected: 编译通过

**Verification:** `AggregateUsageReport(ctx, fromMs, toMs)` 返回按 API Key 聚合的用量汇总

**Rollback Rule:** 如果 SQL 查询慢，确认 api_key_hash 和 timestamp 索引可用

---

### T22: Add handleEnterpriseUsageReport HTTP handler

**Spec 需求映射:** R4（usage-report 端点，JSON + CSV 输出，参数校验，鉴权）

**Files:**
- Modify: `usage-service/internal/httpapi/server.go`（新增 handler 函数）

**Steps:**

- [ ] **Step 1: Implement handleEnterpriseUsageReport**

逻辑描述：
1. 鉴权：调用 `authorizeIfConfigured`
2. 仅支持 GET 方法
3. 从 query string 读取 `fromMs`（必填）、`toMs`（必填）、`format`（可选，默认 json，合法值 json/csv）
4. 校验 fromMs < toMs，否则返回 400
5. 调用 `s.store.AggregateUsageReport(ctx, fromMs, toMs)`
6. 按 format 输出：
   - JSON：`Content-Type: application/json`，写入 `{"fromMs":..., "toMs":..., "items":[...]}`
   - CSV：`Content-Type: text/csv; charset=utf-8`，`Content-Disposition: attachment; filename="usage-report.csv"`，UTF-8 BOM，列头 + 模型按行展开（每个 model 一行）

- [ ] **Step 2: Run Go build**

Run: `go build ./...`
Expected: 编译通过

**Verification:** curl 验证 JSON/CSV 输出格式正确

**Rollback Rule:** 如果 CSV 模型展开行格式不佳，调整为内联 JSON 字符串列

---

### T23: Register usage-report route in handleRoot

**Spec 需求映射:** R4（路由注册）

**Files:**
- Modify: `usage-service/internal/httpapi/server.go`（handleRoot 函数）

**Steps:**

- [ ] **Step 1: Add route prefix matching**

在 enterprise 路由组中追加（在 import-history 之后）：
```go
if strings.HasPrefix(r.URL.Path, "/v0/management/enterprise/usage-report") {
    s.withCORS(s.handleEnterpriseUsageReport)(w, r)
    return
}
```

- [ ] **Step 2: Run Go build**

Run: `go build ./...`
Expected: 编译通过

**Verification:** `GET /v0/management/enterprise/usage-report?fromMs=...&toMs=...` 返回 200

**Rollback Rule:** 如果路由冲突，检查前缀匹配顺序

---

### T24: Write backend tests for usage-report

**Spec 需求映射:** R4（后端测试覆盖正常查询、空范围、参数校验、CSV 格式）

**Files:**
- Modify: `usage-service/internal/httpapi/server_test.go`
- Modify: `usage-service/internal/store/store_test.go`

**Steps:**

- [ ] **Step 1: Add store layer tests**

测试 AggregateUsageReport：
- 插入若干 usage_events 后查询，验证 totalTokens、totalRequests、模型分布正确
- 空时间范围返回空 items
- cacheHitRate 计算正确

- [ ] **Step 2: Add HTTP layer tests**

测试 handleEnterpriseUsageReport：
- 正常查询返回 JSON，结构含 fromMs/toMs/items
- `?format=csv` 返回 CSV，Content-Type 为 text/csv
- `fromMs > toMs` 返回 400
- 缺少 fromMs/toMs 返回 400
- 无鉴权时返回 401

- [ ] **Step 3: Run all tests**

Run: `go test ./...`
Expected: 全部通过

**Verification:** 测试覆盖正常查询、参数校验、CSV 输出、鉴权

**Rollback Rule:** 测试失败则逐个修复，不回滚

---

### T25: Commit Checkpoint 3

**Steps:**

- [ ] **Step 1: Stage and commit**

```bash
git add usage-service/
git commit -m "feat(enterprise-keys): add usage-report aggregation endpoint [AIGC]"
```

**Verification:** `git status` clean，`go test ./...` 通过

---

## Spec-to-Verification Traceability

| Spec Requirement | Task Verification |
|-----------------|-------------------|
| R1 邮箱后端 Schema | T1-T7: struct 含 Email, SQLite migration 幂等, CRUD 支持 email 读写 |
| R1 email migration 幂等 | T2: ALTER TABLE 含 duplicate column 容错 |
| R2 前端类型定义 | T12: EnterpriseKeyBinding 含 email?: string |
| R2 列表邮箱列 | T14: 表格显示邮箱列，空值显示 "-" |
| R2 添加/编辑邮箱输入 | T15: 模态框含邮箱输入框，可选 |
| R3 CSV 导出邮箱列 | T16: 导出 CSV 列顺序为 用户名,邮箱,API Key,部门 |
| R3 CSV 导入按列头匹配 | T8: ParseCSVPreview 匹配 邮箱/email/E-mail 变体 |
| R3 旧格式 CSV 兼容 | T8: 无邮箱列表时 emailIdx=-1，email 为空 |
| R3 共用 Key 邮箱为空 | T8+T16: 空 email 在导入导出中正确处理 |
| R4 usage-report 接口 | T21-T23: 端点返回 JSON/CSV 汇总数据 |
| R4 查询参数 + 校验 | T22: fromMs/toMs 必填，范围校验返回 400 |
| R4 鉴权 | T22: authorizeIfConfigured 调用 |
| R4 统计数据口径一致 | T21: 仅汇总 failed=0 或有 token 的事件 |
| R4 缓存指标 | T21: cachedTokens, totalCacheTokens, cacheHitRate 计算 |
| R5 SQL 聚合性能 | T21: GROUP BY api_key_hash, model，使用现有索引 |
| R5 排序 | T21: 按 department_name, user_name 排序 |
| R0 CPA 地址显示 | T18a: 管理 UI 显示当前监控 CPA 地址 |
| R0 CPA 切换确认 | T18b: 修改 CPA 地址时弹窗确认 |

# Enterprise Key Management Implementation Plan

**Goal:** 为企业管理员提供部门化 API Key 批量导入/导出/管理功能，支持 CSV 导入自动生成 Key、按部门筛选导出、单个 Key 增删，数据 SQLite 持久化，兼容现有 api_key_aliases 数据。

**Architecture:** 后端（Go, SQLite）负责 Key 生成（拼音+随机）、持久化、api_key_aliases 同步；前端（React, Zustand）提供部门管理、CSV 导入预览/确认、Key 列表筛选/导出 UI。两阶段 CSV 导入（generate → 预览 → import），导出由前端生成 CSV。

**Tech Stack:** React 19 + TypeScript + Vite + Zustand + SCSS, Go 1.24 + SQLite (modernc.org/sqlite)

**Risk Level:** medium

**Based on:**
- 设计文档: `.ccg/plan/enterprise-key-management/spec.md`
- 决策记录: `.ccg/plan/enterprise-key-management/decisions.md`

---

## Phase 1: Backend 改造

---

## T1: enterprise_key_bindings 表加 api_key_hash 列

**Spec 需求映射:** C1（api_key_aliases 兼容）、P1（持久化完整性）

**Files:**
- Modify: `usage-service/internal/store/store.go`（schema init + EnterpriseKeyBinding struct + UpsertEnterpriseKeyBindings + LoadEnterpriseKeyBindings）
- Modify: `usage-service/internal/store/store_test.go`（更新测试用 binding 数据含 api_key_hash）

- [ ] **Step 1: 在 EnterpriseKeyBinding struct 中加 APIKeyHash 字段**

在 `EnterpriseKeyBinding` struct 中添加字段：
```go
APIKeyHash string `json:"apiKeyHash"`
```
注意：该字段在 Upsert 时由后端自动计算（SHA256），不需要调用方传入。

- [ ] **Step 2: 在 schema init 中添加 ALTER TABLE + 索引**

在 `init()` 中 enterprise_key_bindings 建表语句位置之后，添加兼容迁移：
```go
// 迁移：添加 api_key_hash 列（如果不存在）
`alter table enterprise_key_bindings add column api_key_hash text not null default ''`,
```
以及索引：`create index if not exists idx_enterprise_key_bindings_api_key_hash on enterprise_key_bindings(api_key_hash)`

- [ ] **Step 3: 更新 UpsertEnterpriseKeyBindings 自动计算 hash**

在 `UpsertEnterpriseKeyBindings` 中，循环内对每个 item 计算 `api_key_hash`：
- 使用 `crypto/sha256` 对 `item.APIKey` 做 SHA256，转 hex string
- 将 `api_key_hash` 传入 INSERT 语句
- 同步调用 `UpsertAPIKeyAliases` 写入/更新 `api_key_aliases`（api_key_hash → user_name）

- [ ] **Step 4: 更新 LoadEnterpriseKeyBindings SELECT 包含 api_key_hash**

修改 SELECT 语句加入 `api_key_hash` 列，并在 Scan 中读取。

- [ ] **Step 5: 更新 store_test.go 验证 api_key_hash**

更新 `TestStoreEnterpriseMetadataPersistence` 中 binding 的断言，验证 api_key_hash 非空且为 64 位 hex。

Run: `go test ./usage-service/internal/store/ -v -run TestStoreEnterpriseMetadata`

**Verification:** 创建 binding 后，api_key_hash 自动填充（SHA256, 64 hex），且 api_key_aliases 表同步存在对应记录。

**Rollback Rule:** 如果 ALTER TABLE 失败或测试不通过，回退 schema 和 struct 变更。

---

## T2: enterprise_import_history 表加 csv_filename + error_details 列

**Spec 需求映射:** I6（导入历史包含 csv_filename、error_details）

**Files:**
- Modify: `usage-service/internal/store/store.go`（EnterpriseImportHistory struct + schema + AppendEnterpriseImportHistory + LoadEnterpriseImportHistory）
- Modify: `usage-service/internal/store/store_test.go`

- [ ] **Step 1: 在 EnterpriseImportHistory struct 中加字段**

```go
CSVFileName  string `json:"csvFileName"`
ErrorDetails string `json:"errorDetails,omitempty"` // JSON array of {row, reason}
```

- [ ] **Step 2: 在 schema init 中添加迁移**

```go
`alter table enterprise_import_history add column csv_filename text not null default ''`,
`alter table enterprise_import_history add column error_details text not null default ''`,
```

- [ ] **Step 3: 更新 AppendEnterpriseImportHistory 的 INSERT 语句**

在 INSERT 和 ON CONFLICT UPDATE 中添加 `csv_filename` 和 `error_details` 列。

- [ ] **Step 4: 更新 LoadEnterpriseImportHistory 的 SELECT + Scan**

添加两列到 SELECT 和 Scan。

- [ ] **Step 5: 更新 store_test.go 验证新字段**

测试中传入 csv_filename 和 error_details，查询后验证值一致。

Run: `go test ./usage-service/internal/store/ -v -run TestStoreEnterpriseMetadata`

**Verification:** 写入 import_history 含 csv_filename 和 error_details，读取后字段值与写入一致。

**Rollback Rule:** 字段添加失败则回退 struct 和 SQL 变更。

---

## T3: 新增 Key 生成 + 拼音转换工具函数

**Spec 需求映射:** I1（上传 CSV 返回含 Key 的预览）、I2（Key 格式正确）

**Files:**
- Create: `usage-service/internal/usage/keygen.go`（新文件：拼音转换 + Key 生成 + CSV 解析）
- Modify: `usage-service/go.mod`（如需添加拼音库依赖）

- [ ] **Step 1: 添加 go-pinyin 依赖**

```bash
cd usage-service && go get github.com/mozillazg/go-pinyin
```
注意：如果已有其他拼音库或需要纯自实现，根据实际 go.sum 情况调整。

- [ ] **Step 2: 实现 pinyinAbbr 函数**

函数签名：`func pinyinAbbr(name string) (string, error)`
逻辑描述：
- 遍历字符串中的每个 rune，跳过非中文字符
- 对每个中文字符调用 pinyin 库取首字母（小写）
- 全部拼接返回，如 "张三丰" → "zsf"
- 如果 name 为空或无中文字符，返回 error

- [ ] **Step 3: 实现 generateAPIKey 函数**

函数签名：`func generateAPIKey(prefix, abbr string) (string, error)`
逻辑描述：
- 使用 `crypto/rand` 生成 30 字节随机数据
- 用 base64 编码或自定义映射（A-Z a-z 0-9）转为 30 位字符串
- 格式：`{prefix}-{abbr}-{random}`
- 验证 prefix 和 abbr 非空

- [ ] **Step 4: 实现 ParseCSVPreview 函数**

函数签名：`func ParseCSVPreview(csvContent []byte, departments []store.EnterpriseDepartment) ([]KeyGenPreview, error)`
逻辑描述：
- 解析 CSV（header: username, department）
- 对每行：查 department name → 取 prefix；查不到则标记 error
- 调用 pinyinAbbr + generateAPIKey
- 返回 `[]KeyGenPreview{UserName, DepartmentName, DepartmentID, GeneratedKey, Status, ErrorReason}`
- status: "ok" | "error" | "warning"

- [ ] **Step 5: 编写 keygen_test.go 单元测试**

Run: `go test ./usage-service/internal/usage/ -v -run TestKeyGen`

**Verification:** "张三" + prefix "sh" → Key 匹配 `/^sh-zs-[A-Za-z0-9]{30}$/`；"张三丰" → 缩写 "zsf"；CSV 中部门不存在时 status="error"。

**Rollback Rule:** 拼音库加载失败时考虑回退到纯自实现 fallback。

---

## T4: 新增 + 重构 HTTP 端点

**Spec 需求映射:** D1-D3（部门 CRUD）、M1-M2（Key 增删）、I1-I4（CSV 导入）、D4（删除部门 → Key 转未分组）

**Files:**
- Modify: `usage-service/internal/httpapi/server.go`（路由注册 + handler 函数）

- [ ] **Step 1: 添加 DELETE /departments/:id 路由**

新增路由处理：`/v0/management/enterprise/departments/` → 解析 path 中的 id，DELETE 方法
Handler 逻辑：
- 删除 `enterprise_departments` 行（by id）
- 将关联的 `enterprise_key_bindings` 的 `department_id` 更新为 `""`（转未分组）
- 返回更新后的 departments 列表

- [ ] **Step 2: 添加 POST /key-bindings（单个新增）路由**

在 `handleEnterpriseKeyBindings` 中加 POST 分支：
- 接收 `{userName, departmentId}` JSON
- 查询 department 获取 prefix
- 调用 `pinyinAbbr` + `generateAPIKey`
- 写入 `enterprise_key_bindings` + 同步 `api_key_aliases`
- 返回生成的 Key 信息

- [ ] **Step 3: 添加 DELETE /key-bindings/:apiKey 路由**

URL 中编码 apiKey（URL encode），DELETE 方法：
- 计算 api_key_hash
- 删除 `enterprise_key_bindings` 行
- 删除 `api_key_aliases` 中对应 hash 的行
- 返回 `{ok: true}`

- [ ] **Step 4: 添加 POST /key-bindings/generate 路由**

接收 CSV 文本（multipart 或 JSON body 中的 csv 字段）：
- 调用 `ParseCSVPreview`
- 返回 `{items: [{userName, departmentName, departmentId, generatedKey, status, errorReason}]}`

- [ ] **Step 5: 添加 POST /key-bindings/import 路由**

接收前端确认后的 items 数组：
- 按 `userName + departmentId` 去重：查询已有 binding，存在则跳过（warning）
- 对 status="ok" 的行，批量写入 `enterprise_key_bindings` + 同步 `api_key_aliases`
- 写入 `enterprise_import_history`
- 返回 `{taskId, totalRows, passedRows, warningRows, errorRows}`

- [ ] **Step 6: 更新 store_test.go 验证新 API 行为**

Run: `go test ./usage-service/internal/store/ -v`

**Verification:** 
- D1-D3: curl 测试部门 CRUD 各路径
- M1: POST /key-bindings 传入 `{userName:"张三", departmentId:"dept_sh"}` → 返回 Key 格式 `sh-zs-[30位随机]`
- M2: DELETE /key-bindings/sh-zs-xxx → api_key_aliases 对应记录也被清除
- I1: POST /generate 传入 CSV → 返回预览数据含 generatedKey
- I4: POST /import → 两表均有记录
- D4: DELETE /departments/dept_sh → 关联 binding 的 department_id 变为空

**Rollback Rule:** 如果单个端点实现有问题，回退该路由注册，不影响其他端点。

---

## Phase 2: 前端清理 + 重建

---

## T5: 清理断裂引用

**Spec 需求映射:** 无（清理性任务，确保构建通过）

**Files:**
- Modify: `src/router/MainRoutes.tsx`（移除 EnterpriseKeysPage import + 路由）
- Modify: `src/services/api/index.ts`（移除 enterpriseMetadata export）
- Modify: `src/stores/index.ts`（移除 useEnterpriseKeyStore export）
- Modify: `src/types/index.ts`（移除 enterpriseKey export）

- [ ] **Step 1: 回退 MainRoutes.tsx 中企业 Key 路由**

移除 `import { EnterpriseKeysPage } from '@/pages/EnterpriseKeysPage'` 和对应路由项。

- [ ] **Step 2: 回退 api/index.ts、stores/index.ts、types/index.ts 中断裂导出**

移除 `export * from './enterpriseMetadata'`、`export { useEnterpriseKeyStore, UNGROUPED_DEPARTMENT_ID } from './useEnterpriseKeyStore'`、`export * from './enterpriseKey'`。

- [ ] **Step 3: 验证构建通过**

Run: `npm run type-check` 或 `npx tsc --noEmit`
Expected: 无编译错误

**Verification:** `tsc --noEmit` 零错误。

**Rollback Rule:** 如果清理后出现其他编译错误（非企业 Key 相关），评估是否需要保留部分引用再逐步移除。

---

## T6: 类型定义 + API 客户端

**Spec 需求映射:** 所有前端验收标准的前置依赖（数据层）

**Files:**
- Create: `src/types/enterpriseKey.ts`
- Create: `src/services/api/enterpriseKeys.ts`

- [ ] **Step 1: 定义 TypeScript 类型**

在 `src/types/enterpriseKey.ts` 中定义：
```typescript
export interface EnterpriseDepartment {
  id: string;
  name: string;
  prefix: string;
  sortOrder: number;
  enabled: boolean;
  system: boolean;
  updatedBy?: string;
  createdAtMs: number;
  updatedAtMs: number;
}

export interface EnterpriseKeyBinding {
  apiKey: string;
  apiKeyHash: string;
  userName: string;
  departmentId: string;
  source: 'import' | 'manual';
  departmentResolvedBy: 'csv' | 'manual';
  updatedBy?: string;
  createdAtMs: number;
  updatedAtMs: number;
}

export interface EnterpriseImportHistory {
  taskId: string;
  csvFileName: string;
  totalRows: number;
  passedRows: number;
  warningRows: number;
  errorRows: number;
  errorDetails?: string;
  status: 'running' | 'done' | 'partial';
  updatedBy?: string;
  createdAtMs: number;
  completedAtMs?: number;
}

export interface KeyGenPreviewItem {
  userName: string;
  departmentName: string;
  departmentId: string;
  generatedKey?: string;
  status: 'ok' | 'error' | 'warning';
  errorReason?: string;
}

// 未分组虚拟部门 ID
export const UNGROUPED_DEPARTMENT_ID = '__ungrouped__';
```

- [ ] **Step 2: 创建 API 客户端**

在 `src/services/api/enterpriseKeys.ts` 中实现，使用 `apiClient` 封装各端点：

```typescript
import { apiClient } from './client';
import type { EnterpriseDepartment, EnterpriseKeyBinding, EnterpriseImportHistory, KeyGenPreviewItem } from '@/types/enterpriseKey';

const BASE = '/v0/management/enterprise';

export const enterpriseKeysApi = {
  // Departments
  listDepartments: () => apiClient.get<{items: EnterpriseDepartment[]}>(`${BASE}/departments`),
  upsertDepartments: (items: EnterpriseDepartment[]) => apiClient.put<{items: EnterpriseDepartment[]}>(`${BASE}/departments`, {items}),
  deleteDepartment: (id: string) => apiClient.delete<{items: EnterpriseDepartment[]}>(`${BASE}/departments/${encodeURIComponent(id)}`),

  // Key Bindings
  listKeyBindings: () => apiClient.get<{items: EnterpriseKeyBinding[]}>(`${BASE}/key-bindings`),
  createKeyBinding: (data: {userName: string; departmentId: string}) => apiClient.post<EnterpriseKeyBinding>(`${BASE}/key-bindings`, data),
  deleteKeyBinding: (apiKey: string) => apiClient.delete(`${BASE}/key-bindings/${encodeURIComponent(apiKey)}`),

  // CSV Import
  generatePreview: (csvContent: string) => apiClient.post<{items: KeyGenPreviewItem[]}>(`${BASE}/key-bindings/generate`, {csv: csvContent}),
  importKeys: (items: KeyGenPreviewItem[]) => apiClient.post<{taskId: string}> (`${BASE}/key-bindings/import`, {items}),

  // Import History
  listImportHistory: (limit?: number) => apiClient.get<{items: EnterpriseImportHistory[]}>(`${BASE}/import-history`, {params: {limit}}),
};
```

- [ ] **Step 3: 在 types/index.ts 和 services/api/index.ts 中注册导出**

`src/types/index.ts` 加 `export * from './enterpriseKey'`
`src/services/api/index.ts` 加 `export * from './enterpriseKeys'`

- [ ] **Step 4: 验证类型检查通过**

Run: `npx tsc --noEmit`
Expected: 无类型错误

**Verification:** API 客户端所有方法签名与后端端点一致（GET/POST/PUT/DELETE 匹配），TypeScript 编译零错误。

**Rollback Rule:** 类型定义与后端 struct 不一致时，以 spec.md 为准修正。

---

## T7: Zustand Store

**Spec 需求映射:** 所有前端验收标准的状态管理层

**Files:**
- Create: `src/stores/useEnterpriseKeyStore.ts`

- [ ] **Step 1: 创建 useEnterpriseKeyStore**

Store 包含状态和操作：

```typescript
// State
departments: EnterpriseDepartment[]
keyBindings: EnterpriseKeyBinding[]
importHistory: EnterpriseImportHistory[]
loading: boolean
error: string | null

// Actions（伪代码 + 逻辑描述）
fetchDepartments() → enterpriseKeysApi.listDepartments() → set departments
upsertDepartments(items) → enterpriseKeysApi.upsertDepartments(items) → 刷新 departments
deleteDepartment(id) → enterpriseKeysApi.deleteDepartment(id) → 刷新 departments + bindings

fetchKeyBindings() → enterpriseKeysApi.listKeyBindings() → set keyBindings
createKeyBinding(userName, departmentId) → enterpriseKeysApi.createKeyBinding({...}) → 追加到 keyBindings
deleteKeyBinding(apiKey) → enterpriseKeysApi.deleteKeyBinding(apiKey) → 从 keyBindings 中移除

generatePreview(csvContent) → enterpriseKeysApi.generatePreview(csvContent) → 返回预览数据（不存 store，由页面组件管理）
importKeys(items) → enterpriseKeysApi.importKeys(items) → 刷新 bindings + history

fetchImportHistory(limit?) → enterpriseKeysApi.listImportHistory(limit) → set importHistory
```

- [ ] **Step 2: 在 stores/index.ts 中注册导出**

`src/stores/index.ts` 加 `export { useEnterpriseKeyStore } from './useEnterpriseKeyStore'`

- [ ] **Step 3: 验证编译**

Run: `npx tsc --noEmit`

**Verification:** Store 所有方法签名与 API 客户端一致，各 action 正确调用对应的 API 方法并更新 state。

**Rollback Rule:** Store 逻辑有误时，对照 API 客户端方法签名修正。

---

## T8: 企业 Key 管理页面

**Spec 需求映射:** D1-D4（部门管理 UI）、I1-I6（CSV 导入 UI）、E1-E4（CSV 导出 UI）、M1-M2（Key 增删 UI）、C2（未分组展示）

**Files:**
- Create: `src/pages/EnterpriseKeysPage.tsx`
- Create: `src/pages/EnterpriseKeysPage.module.scss`

- [ ] **Step 1: 页面主体结构 + 部门筛选 + 按钮栏**

页面布局（伪代码逻辑）：
```
<PageContainer>
  标题栏：部门管理按钮（打开部门弹窗）
  工具栏：
    - 部门下拉（全部 / 各部门 / 未分组）→ 控制表格筛选
    - "导入CSV" 按钮 → 打开导入弹窗
    - "新增Key" 按钮 → 打开新增弹窗
    - "导出选中" 按钮 → 下载 CSV

  数据表格：
    列：复选框 | 用户名 | API Key | 部门 | 操作(删除)
    行数据来源：
      - enterprise_key_bindings（按 departmentId 筛选或全部）
      - api_key_aliases 中无 binding 的（仅"未分组"筛选时显示）
    底部：已选 N 项
```

- [ ] **Step 2: 部门管理弹窗**

弹窗内容：
- 已有部门列表（可编辑名称/前缀、可删除）
- 新增行：输入名称 + 前缀 → 添加
- 使用 `useEnterpriseKeyStore.upsertDepartments` / `deleteDepartment`

- [ ] **Step 3: CSV 导入弹窗（两阶段）**

阶段一（上传 + 预览）：
- 文件选择器读取 CSV → 调用 `generatePreview`
- 展示预览表格（用户名、部门、生成的 Key、状态标记）
- ok 行绿色，error 行红色（显示错误原因），warning 行黄色

阶段二（确认导入）：
- 用户确认 → 调用 `importKeys(okItems)`
- 显示导入结果摘要（通过/警告/错误数）
- 刷新列表

- [ ] **Step 4: 新增 Key 弹窗**

弹窗内容：
- 部门下拉（从 departments 列表）
- 用户名输入框
- 确认 → 调用 `createKeyBinding`

- [ ] **Step 5: 表格行操作**

- 删除按钮：确认弹窗 → `deleteKeyBinding(apiKey)`
- 未分组行（仅 alias）：无 apiKey 列显示，无删除按钮（不支持未绑定的 alias 删除）

- [ ] **Step 6: CSV 导出功能**

点击"导出选中"：
- 收集所有勾选行的数据（userName, apiKey, departmentName）
- 生成 CSV 字符串（BOM + 三列）
- 调用 `downloadBlob({filename, blob, withBom: true})`
- 未分组行导出时 apiKey 列为空

- [ ] **Step 7: 导入历史面板（可选折叠区域）**

展示 `importHistory` 列表：
- 任务时间、文件名、总行数/通过/警告/错误、状态
- 错误详情可展开

- [ ] **Step 8: 验证构建**

Run: `npm run build`
Expected: 构建成功，`dist/index.html` 生成

**Verification:** 
- D1-D3: 部门管理弹窗新增/编辑/删除后列表刷新
- I1-I5: CSV 上传 → 预览展示 Key + 状态 → 确认导入 → 列表出现新 Key
- E1-E4: 按部门筛选 → 勾选 → 导出 CSV 文件内容含选中的 用户名,Key,部门
- M1-M2: 新增 Key / 删除 Key 后列表即时更新
- C2: 选择"未分组" → 展示仅有 alias 无 binding 的记录

**Rollback Rule:** 页面整体不工作时，检查 store 和 API 客户端是否正常工作，逐层排查。

---

## T9: 导航 + 路由 + i18n

**Spec 需求映射:** 无直接 spec 映射（UI 基础设施）

**Files:**
- Modify: `src/router/MainRoutes.tsx`（添加路由）
- Modify: `src/components/layout/MainLayout.tsx`（添加导航项）
- Modify: `src/i18n/locales/zh-CN.json`（添加翻译条目）

- [ ] **Step 1: 添加路由**

在 `MainRoutes.tsx` 中：
- import EnterpriseKeysPage
- 添加 `{ path: '/enterprise-keys', element: <EnterpriseKeysPage /> }`

- [ ] **Step 2: 添加导航项**

在 `MainLayout.tsx` navItems 数组中：
```tsx
{ path: '/enterprise-keys', label: t('nav.enterprise_keys'), icon: sidebarIcons.config }
```

- [ ] **Step 3: 添加/补充 i18n 翻译**

`src/i18n/locales/zh-CN.json` 中：
- `nav.enterprise_keys`: "企业 Key 管理"
- `enterprise_keys` 块：部门管理相关文案（department_section_title, department_name_placeholder, ...）
- `enterprise_keys` 块：导入相关文案（import_csv_btn, preview_title, confirm_import, ...）
- `enterprise_keys` 块：导出相关文案（export_selected, export_empty, ...）
- `enterprise_keys` 块：通用操作文案（add_key, delete_confirm, ungrouped_label, ...）

- [ ] **Step 4: 验证构建**

Run: `npm run build`

**Verification:** 导航栏出现"企业 Key 管理"入口，点击跳转到 `/enterprise-keys` 页面，页面文案均为中文。

**Rollback Rule:** 导航/路由正常但页面空白，回到 T8 排查页面组件。

---

## Phase 3: 集成验证

---

## T10: management.html 同步

**Spec 需求映射:** P1（Full Docker 模式下可用）

**Files:**
- Modify: `usage-service/internal/httpapi/web/management.html`（用构建产物覆盖）

- [ ] **Step 1: 构建前端**

Run: `npm run build`
验证 `dist/index.html` 已生成。

- [ ] **Step 2: 同步到 usage-service**

将 `dist/index.html` 内容复制到 `usage-service/internal/httpapi/web/management.html`。

- [ ] **Step 3: 验证 Go 构建通过**

Run: `cd usage-service && go build ./...`
Expected: embed 成功，二进制编译通过。

**Verification:** Go 构建成功，二进制启动后访问 `/management.html` 能加载企业 Key 管理页面。

**Rollback Rule:** embed 失败时检查 `management.html` 文件大小和编码。

---

## T11: 端到端验证

**Spec 需求映射:** 全部验收标准

**Files:**
- Run: `npm run test`（前端 vitest）
- Run: `go test ./usage-service/...`（后端测试）
- 手动端到端检查（如服务可启动）

- [ ] **Step 1: 运行后端测试**

Run: `go test ./usage-service/...`
Expected: 全部 PASS

- [ ] **Step 2: 运行前端测试**

Run: `npm test`
Expected: 全部 PASS 或合理的 skip

- [ ] **Step 3: 运行前端类型检查 + lint**

Run: `npm run type-check && npm run lint`
Expected: 零错误

- [ ] **Step 4: 手动验收检查清单（Approval Gate）**

如果服务可启动，手动验证：
- 部门新增/编辑/删除
- CSV 上传 → 预览 → 确认导入
- 导入结果展示 + 导入历史记录
- 按部门筛选 Key 列表
- 勾选导出 CSV
- 单个新增 Key + 单个删除 Key
- 未分组展示历史 alias
- 重启服务后数据完整

**Verification:** 
| Spec 需求 | 验收方式 |
|-----------|---------|
| D1-D4 部门管理 | 手动操作 + API 测试 |
| I1-I6 CSV 导入 | 手动上传 CSV + 验证结果 |
| E1-E4 CSV 导出 | 手动勾选导出 + 检查文件内容 |
| M1-M2 Key 管理 | 手动新增/删除 + API 验证 |
| P1-P2 持久化 | 重启服务后数据完整 |
| C1-C3 兼容性 | 旧 alias 不受影响、未分组正常展示 |

**Rollback Rule:** 如果端到端有断裂，按 Phase 1/2/3 逐层排查。

---

## Spec-to-Verification 可追溯性

| Spec 需求 | Task Verification |
|-----------|-------------------|
| D1-D4 部门管理 | T4（API） + T8 Step 1-2（UI） |
| I1 上传 CSV 返回预览 | T3 Step 4（ParseCSVPreview）+ T4 Step 4（generate 端点） |
| I2 Key 格式正确 | T3 Step 3（generateAPIKey） |
| I3 重复导入跳过 | T4 Step 5（import 去重逻辑） |
| I4 两表同步写入 | T1 Step 3（Upsert synced to api_key_aliases） |
| I5 部门名不存在 | T3 Step 4（ParseCSVPreview error status） |
| I6 导入历史含 csv_filename/error_details | T2（schema 扩展）+ T8 Step 7（UI 展示） |
| E1-E4 CSV 导出 | T8 Step 6（前端生成 CSV） |
| M1-M2 Key 增删 | T4 Step 2-3（API）+ T8 Step 4-5（UI） |
| P1-P2 持久化 | T1-T2（SQLite schema）+ T11（端到端重启测试） |
| C1-C3 兼容性 | T1 Step 3（api_key_aliases 同步）+ T8 Step 5（未分组展示） |

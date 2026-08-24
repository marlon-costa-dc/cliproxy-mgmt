# 企业 Key 管理设计与使用说明

本文档说明本次“企业化 API Key 管理”能力的设计方案、数据模型、流程与使用方法。

## 1. 背景与目标

原有 `api-keys` 配置是纯字符串数组，无法表达企业管理所需的结构化信息（如用户名、部门、别名、导入来源）。

本次目标：

- 支持批量导入（CSV）并自动生成 Key
- 支持按部门分组管理、前缀治理
- 支持选中用户批量导出（分组、别名、API Key）
- 保持对现有后端 `/api-keys` 协议兼容（不破坏旧版本）

## 2. 总体方案

采用 **“主数据兼容 + 元数据旁路存储”** 的双层方案：

1. 主数据层（兼容层）
   - 继续使用 `/api-keys` 的字符串数组协议。
   - 导入时通过 `list -> merge dedupe -> replace` 写回。

2. 元数据层（企业管理层）
   - 在 `usage-service` 侧独立持久化：`部门`、`Key 绑定关系`、`导入历史`。
   - 通过 `apiKey` 关联用户名/部门/别名等信息。
   - 前端通过 enterprise metadata API 读写元数据。

这样既能满足企业化治理，又不需要同步改造后端配置结构。

## 3. 数据模型

### 3.1 前端类型

位置：`src/types/enterpriseKey.ts`

- `Department`
  - `id`, `name`, `prefix`, `system`, `enabled`, `createdAt`, `updatedAt`
- `KeyBinding`
  - `apiKey`, `username`, `departmentId`, `alias`, `source`, `departmentResolvedBy`, `createdAt`, `updatedAt`
- `EnterpriseImportHistoryItem`
  - `id`, `createdAt`, `totalRows`, `passedRows`, `warningRows`, `errorRows`

### 3.2 usage-service 持久化表

位置：`usage-service/internal/store/store.go`

- `enterprise_departments`
- `enterprise_key_bindings`
- `enterprise_import_history`

### 内置部门

- `ungrouped` 为系统保留部门（不可删除）
- 未知部门/空部门默认回落到 `ungrouped`

## 4. 后端接口（元数据）

位置：`usage-service/internal/httpapi/server.go`

- `GET/PUT /v0/management/enterprise/departments`
- `GET/PUT /v0/management/enterprise/key-bindings`
- `GET/POST /v0/management/enterprise/import-history`

## 5. 关键流程

### 5.1 导入预检

位置：`src/utils/enterpriseKeyImport.ts`

- 输入：CSV（`username,department,alias,apiKey,prefix`）
- 校验：
  - `username` 必填
  - 前缀格式校验（小写字母/数字/中划线）
  - 冲突校验（已有 key、文件内重复）
- 输出：`rows`（通过）、`warnings`、`errors`

### 5.2 自动生成 Key

位置：`src/utils/enterpriseKeyGenerator.ts`

- 格式：`<prefix>-<random>`
- 默认随机段：base62
- 生成时检查冲突并重试

### 5.3 提交导入

位置：`src/services/enterpriseKeyManagement.ts`

- 流程：
  1. 读取 `/api-keys`
  2. 合并去重
  3. `replace` 回写
  4. 生成/更新 `KeyBinding`
  5. 通过元数据 API 持久化部门、绑定、导入历史

### 5.4 批量导出

位置：`src/services/enterpriseKeyManagement.ts`

- 输入：`selectedUsernames + bindings + departments`
- 输出：CSV（`username,department,alias,apiKey`）
- 编码：`UTF-8 BOM`（Excel 直接打开中文不乱码）

## 6. 页面与入口

### 6.1 独立管理页

- 路由：`/enterprise-keys`
- 页面：`src/pages/EnterpriseKeysPage.tsx`
- 导航：侧边栏 “企业 Key 管理”

### 6.2 页面能力

- 部门管理
  - 新增部门
  - 编辑部门名/前缀
  - 前缀唯一性校验
  - 部门排序（上移/下移）
  - 删除部门（关联 Key 自动回落 `ungrouped`）
- 导入治理
  - 选择 CSV 文件导入（也支持直接粘贴 CSV 文本）
  - CSV 预检
  - 仅导入通过行
  - 错误明细下载
- 日常运营
  - 选中 Key 批量分配部门
  - 选中用户导出 CSV
  - 导入历史记录（统计维度）

## 7. 使用方法

### 7.1 构建并同步到 usage-service

如果你通过 `usage-service` 内嵌面板访问，前端更新后需同步 `management.html`：

```bash
npm run build
copy dist\index.html usage-service\internal\httpapi\web\management.html
cd usage-service
go build -o dist/cpa-manager-usage-service.exe ./cmd/cpa-manager
```

> 说明：`go build ./...` 仅做编译检查，不会自动把可执行文件输出到发布目录。

### 7.2 在页面操作导入导出

1. 打开侧边栏：`企业 Key 管理`
2. 先维护部门与前缀
3. 在导入区点击“选择 CSV 文件”，选择本地 CSV
   - 可直接使用示例：`docs/examples/enterprise-import-realistic.csv`
4. 点击“预检”，确认通过/警告/错误统计
5. 选择是否“仅导入通过行（推荐）”
6. 点击“确认导入”
7. 勾选用户对应的 Key，点击“导出选中用户”

### 7.3 Excel 打开不乱码说明

- 当前导出的 CSV 默认带 `UTF-8 BOM`，Windows Excel 可直接双击打开。
- 如仍出现乱码，请确认：
  - Excel 使用“数据 -> 自文本/CSV 导入”并选择 UTF-8；或
  - 操作系统区域/Excel 默认编码策略没有被强制为 ANSI。

## 8. 约束与说明

- 主数据 (`api-keys`) 与元数据（部门/绑定/历史）是分层存储。
- 导出依赖 `KeyBinding.username`；无用户名绑定的 key 不会进入用户导出结果。
- 导入历史记录统计信息；详细错误可通过“下载错误明细”导出。

## 9. 后续建议

- 导入历史支持“回看明细”与关键字检索
- 部门管理增加权限控制与审计
- 增加导出文件字段配置（按企业需求裁剪列）

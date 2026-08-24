#!/usr/bin/env python3
"""Apply all T1 edits to store.go using exact substring matching."""

import os

FILE = os.path.join(os.path.dirname(os.path.abspath(__file__)), "store.go")
with open(FILE, "r", encoding="utf-8") as f:
    content = f.read()

ok = 0
fail = 0

def replace(old, new, label):
    global content, ok, fail
    if old not in content:
        print(f"FAIL [{label}]: not found in file")
        print(f"  Looking for: {repr(old[:150])}")
        # Try to find similar text
        idx = content.find(old[:40])
        if idx >= 0:
            print(f"  Found prefix at position {idx}, nearby:")
            print(f"  {repr(content[idx:idx+200])}")
        fail += 1
        return False
    content = content.replace(old, new, 1)
    print(f"OK: {label}")
    ok += 1
    return True


# 1. Add crypto/sha256 to imports - unique because "context" + "database/sql" on adjacent lines
replace(
    '\t"context"\n\t"database/sql"',
    '\t"context"\n\t"crypto/sha256"\n\t"database/sql"',
    "1. import sha256",
)

# 2. Add migration ALTER TABLE + index. Match unique pattern between user_name index
#    and enterprise_import_history table creation.
replace(
    " on enterprise_key_bindings(user_name, updated_at_ms)',\n\t\t'create table if not exists enterprise_import_history (",
    " on enterprise_key_bindings(user_name, updated_at_ms)',\n"
    "\t\t'alter table enterprise_key_bindings add column api_key_hash text not null default ''''',\n"
    "\t\t'create index if not exists idx_enterprise_key_bindings_api_key_hash on enterprise_key_bindings(api_key_hash)',\n"
    "\t\t'create table if not exists enterprise_import_history (",
    "2. migration + index",
)

# 3. Update INSERT SQL: add api_key_hash after api_key in column list
replace(
    "api_key, user_name, department_id, source, department_resolved_by, updated_by, created_at_ms, updated_at_ms\n"
    "\t) values(?, ?, ?, ?, ?, ?, ?, ?)\n"
    "\ton conflict(api_key) do update set\n"
    "\t\tuser_name=excluded.user_name,",
    "api_key, api_key_hash, user_name, department_id, source, department_resolved_by, updated_by, created_at_ms, updated_at_ms\n"
    "\t) values(?, ?, ?, ?, ?, ?, ?, ?, ?)\n"
    "\ton conflict(api_key) do update set\n"
    "\t\tapi_key_hash=excluded.api_key_hash,\n"
    "\t\tuser_name=excluded.user_name,",
    "3. insert sql + values + conflict",
)

# 4. Replace the ExecContext call line with sha256 computation + updated call
replace(
    "if _, err := stmt.ExecContext(ctx, apiKey, strings.TrimSpace(item.UserName), deptID, source, resolvedBy, nullString(strings.TrimSpace(item.UpdatedBy)), createdAt, updatedAt); err != nil {",
    "sum := sha256.Sum256([]byte(apiKey))\n"
    "\t\t\tapiKeyHash := fmt.Sprintf(\"%x\", sum)\n"
    "\t\t\titem.APIKeyHash = apiKeyHash\n"
    "\t\t\tif _, err := stmt.ExecContext(ctx, apiKey, apiKeyHash, strings.TrimSpace(item.UserName), deptID, source, resolvedBy, nullString(strings.TrimSpace(item.UpdatedBy)), createdAt, updatedAt); err != nil {",
    "4. sha256 + exec",
)

# 5. After ExecContext return err line, add alias sync + insert helper method before AppendEnterpriseImportHistory
replace(
    "\t\t\treturn err\n"
    "\t\t}\n"
    "\t}\n"
    "\treturn tx.Commit()\n"
    "}\n"
    "\n"
    "func (s *Store) AppendEnterpriseImportHistory",
    "\t\t\treturn err\n"
    "\t\t}\n"
    "\t\t// Sync to api_key_aliases\n"
    "\t\tif err := s.upsertAPIKeyAliasInTx(ctx, tx, apiKeyHash, strings.TrimSpace(item.UserName), now); err != nil {\n"
    "\t\t\treturn err\n"
    "\t\t}\n"
    "\t}\n"
    "\treturn tx.Commit()\n"
    "}\n"
    "\n"
    "func (s *Store) upsertAPIKeyAliasInTx(ctx context.Context, tx *sql.Tx, apiKeyHash, alias string, now int64) error {\n"
    "\tstmt, err := tx.PrepareContext(ctx, 'insert into api_key_aliases(api_key_hash, alias, updated_at_ms) values(?, ?, ?) on conflict(api_key_hash) do update set alias=excluded.alias, updated_at_ms=excluded.updated_at_ms')\n"
    "\tif err != nil { return err }\n"
    "\tdefer stmt.Close()\n"
    "\t_, err = stmt.ExecContext(ctx, apiKeyHash, alias, now)\n"
    "\treturn err\n"
    "}\n"
    "\n"
    "func (s *Store) AppendEnterpriseImportHistory",
    "5. alias sync + helper",
)

# 6. Update SELECT in LoadEnterpriseKeyBindings
replace(
    "select api_key, user_name, department_id, source, department_resolved_by, updated_by, created_at_ms, updated_at_ms\n"
    "\t\tfrom enterprise_key_bindings",
    "select api_key, api_key_hash, user_name, department_id, source, department_resolved_by, updated_by, created_at_ms, updated_at_ms\n"
    "\t\tfrom enterprise_key_bindings",
    "6. select columns",
)

# 7. Update Scan in LoadEnterpriseKeyBindings
replace(
    "rows.Scan(&item.APIKey, &item.UserName, &item.DepartmentID, &item.Source, &item.DepartmentResolvedBy, &updatedBy, &item.CreatedAtMS, &item.UpdatedAtMS)",
    "rows.Scan(&item.APIKey, &item.APIKeyHash, &item.UserName, &item.DepartmentID, &item.Source, &item.DepartmentResolvedBy, &updatedBy, &item.CreatedAtMS, &item.UpdatedAtMS)",
    "7. scan arguments",
)

with open(FILE, "w", encoding="utf-8") as f:
    f.write(content)

print(f"\nResult: {ok} OK, {fail} FAIL")

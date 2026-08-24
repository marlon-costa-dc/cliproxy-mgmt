import { apiClient } from './client';
import type {
  EnterpriseDepartment,
  EnterpriseImportHistory,
  EnterpriseKeyBinding,
  KeyGenPreviewItem,
} from '@/types/enterpriseKey';

const BASE = '/enterprise';

export interface EnterpriseDepartmentsResponse {
  items: EnterpriseDepartment[];
}

export interface EnterpriseKeyBindingsResponse {
  items: EnterpriseKeyBinding[];
}

export interface EnterpriseImportPreviewResponse {
  items: KeyGenPreviewItem[];
}

export interface EnterpriseImportResult {
  taskId: string;
  totalRows: number;
  passedRows: number;
  warningRows: number;
  errorRows: number;
}

export interface EnterpriseImportHistoryResponse {
  items: EnterpriseImportHistory[];
}

export const enterpriseKeysApi = {
  listDepartments: () =>
    apiClient.get<EnterpriseDepartmentsResponse>(`${BASE}/departments`),

  upsertDepartments: (items: EnterpriseDepartment[]) =>
    apiClient.put<EnterpriseDepartmentsResponse>(`${BASE}/departments`, { items }),

  deleteDepartment: (id: string) =>
    apiClient.delete<EnterpriseDepartmentsResponse>(
      `${BASE}/departments/${encodeURIComponent(id)}`,
    ),

  listKeyBindings: () =>
    apiClient.get<EnterpriseKeyBindingsResponse>(`${BASE}/key-bindings`),

  createKeyBinding: (data: { userName: string; departmentId: string; apiKey?: string; email?: string }) =>
    apiClient.post<EnterpriseKeyBinding>(`${BASE}/key-bindings`, data),

  deleteKeyBinding: (apiKey: string) =>
    apiClient.delete<{ ok: boolean }>(`${BASE}/key-bindings/${encodeURIComponent(apiKey)}`),

  updateKeyBinding: (apiKey: string, data: { userName: string; departmentId: string; email?: string }) =>
    apiClient.patch<EnterpriseKeyBinding>(`${BASE}/key-bindings/${encodeURIComponent(apiKey)}`, data),

  deleteKeyBindings: (apiKeys: string[]) =>
    apiClient.delete<{ ok: boolean }>(`${BASE}/key-bindings`, { data: { apiKeys } }),

  generatePreview: (csvContent: string) =>
    apiClient.post<EnterpriseImportPreviewResponse>(`${BASE}/key-bindings/generate`, {
      csv: csvContent,
    }),

  importKeys: (items: KeyGenPreviewItem[]) =>
    apiClient.post<EnterpriseImportResult>(`${BASE}/key-bindings/import`, { items }),

  listImportHistory: (limit?: number) =>
    apiClient.get<EnterpriseImportHistoryResponse>(`${BASE}/import-history`, {
      params: limit === undefined ? undefined : { limit },
    }),
};

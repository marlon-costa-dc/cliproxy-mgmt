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
  email?: string;
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
  email?: string;
  departmentName: string;
  departmentId: string;
  generatedKey?: string;
  status: 'ok' | 'error' | 'warning';
  errorReason?: string;
}

export const UNGROUPED_DEPARTMENT_ID = '__ungrouped__';

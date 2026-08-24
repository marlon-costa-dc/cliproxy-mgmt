import { create } from 'zustand';
import { enterpriseKeysApi } from '@/services/api';
import type {
  EnterpriseDepartment,
  EnterpriseImportHistory,
  EnterpriseKeyBinding,
  KeyGenPreviewItem,
} from '@/types/enterpriseKey';
import { UNGROUPED_DEPARTMENT_ID } from '@/types/enterpriseKey';

export type EnterpriseDepartmentFilter = 'all' | typeof UNGROUPED_DEPARTMENT_ID | string;

interface EnterpriseKeyStoreState {
  departments: EnterpriseDepartment[];
  keyBindings: EnterpriseKeyBinding[];
  importHistory: EnterpriseImportHistory[];
  loading: boolean;
  error: string | null;
  selectedDepartmentId: EnterpriseDepartmentFilter;
  selectedApiKeys: string[];
  setSelectedDepartmentId: (departmentId: EnterpriseDepartmentFilter) => void;
  setSelectedApiKeys: (apiKeys: string[]) => void;
  clearSelection: () => void;
  getFilteredKeyBindings: () => EnterpriseKeyBinding[];
  fetchDepartments: () => Promise<EnterpriseDepartment[]>;
  upsertDepartments: (items: EnterpriseDepartment[]) => Promise<EnterpriseDepartment[]>;
  deleteDepartment: (id: string) => Promise<EnterpriseDepartment[]>;
  fetchKeyBindings: () => Promise<EnterpriseKeyBinding[]>;
  createKeyBinding: (userName: string, departmentId: string, apiKey?: string, email?: string) => Promise<EnterpriseKeyBinding>;
  updateKeyBinding: (apiKey: string, userName: string, departmentId: string, email?: string) => Promise<EnterpriseKeyBinding>;
  deleteKeyBinding: (apiKey: string) => Promise<void>;
  deleteKeyBindings: (apiKeys: string[]) => Promise<void>;
  generatePreview: (csvContent: string) => Promise<KeyGenPreviewItem[]>;
  importKeys: (items: KeyGenPreviewItem[]) => Promise<void>;
  fetchImportHistory: (limit?: number) => Promise<EnterpriseImportHistory[]>;
}

const toMessage = (error: unknown, fallback: string): string =>
  error instanceof Error ? error.message : fallback;

export const useEnterpriseKeyStore = create<EnterpriseKeyStoreState>((set, get) => ({
  departments: [],
  keyBindings: [],
  importHistory: [],
  loading: false,
  error: null,
  selectedDepartmentId: 'all',
  selectedApiKeys: [],

  setSelectedDepartmentId: (departmentId) => set({ selectedDepartmentId: departmentId }),

  setSelectedApiKeys: (apiKeys) => set({ selectedApiKeys: apiKeys }),

  clearSelection: () => set({ selectedApiKeys: [] }),

  getFilteredKeyBindings: () => {
    const { keyBindings, selectedDepartmentId } = get();
    if (selectedDepartmentId === 'all') {
      return keyBindings;
    }
    if (selectedDepartmentId === UNGROUPED_DEPARTMENT_ID) {
      return keyBindings.filter((item) => !item.departmentId);
    }
    return keyBindings.filter((item) => item.departmentId === selectedDepartmentId);
  },

  fetchDepartments: async () => {
    set({ loading: true, error: null });
    try {
      const response = await enterpriseKeysApi.listDepartments();
      const validDepartmentIds = new Set(response.items.map((item) => item.id));
      set((state) => {
        const shouldResetFilter =
          state.selectedDepartmentId !== 'all' &&
          state.selectedDepartmentId !== UNGROUPED_DEPARTMENT_ID &&
          !validDepartmentIds.has(state.selectedDepartmentId);
        return {
          departments: response.items,
          selectedDepartmentId: shouldResetFilter ? 'all' : state.selectedDepartmentId,
          loading: false,
        };
      });
      return response.items;
    } catch (error) {
      const message = toMessage(error, 'Failed to fetch departments');
      set({ loading: false, error: message });
      throw error;
    }
  },

  upsertDepartments: async (items) => {
    set({ loading: true, error: null });
    try {
      const response = await enterpriseKeysApi.upsertDepartments(items);
      set({ departments: response.items, loading: false });
      return response.items;
    } catch (error) {
      const message = toMessage(error, 'Failed to save departments');
      set({ loading: false, error: message });
      throw error;
    }
  },

  deleteDepartment: async (id) => {
    set({ loading: true, error: null });
    try {
      const [departmentsResponse, keyBindingsResponse] = await Promise.all([
        enterpriseKeysApi.deleteDepartment(id),
        enterpriseKeysApi.listKeyBindings(),
      ]);
      set({
        departments: departmentsResponse.items,
        keyBindings: keyBindingsResponse.items,
        loading: false,
      });
      return departmentsResponse.items;
    } catch (error) {
      const message = toMessage(error, 'Failed to delete department');
      set({ loading: false, error: message });
      throw error;
    }
  },

  fetchKeyBindings: async () => {
    set({ loading: true, error: null });
    try {
      const response = await enterpriseKeysApi.listKeyBindings();
      set({ keyBindings: response.items, loading: false });
      return response.items;
    } catch (error) {
      const message = toMessage(error, 'Failed to fetch key bindings');
      set({ loading: false, error: message });
      throw error;
    }
  },

  createKeyBinding: async (userName, departmentId, apiKey, email) => {
    set({ loading: true, error: null });
    try {
      const created = await enterpriseKeysApi.createKeyBinding({ userName, departmentId, apiKey, email });
      set((state) => ({
        keyBindings: [created, ...state.keyBindings],
        loading: false,
      }));
      return created;
    } catch (error) {
      const message = toMessage(error, 'Failed to create key binding');
      set({ loading: false, error: message });
      throw error;
    }
  },

  updateKeyBinding: async (apiKey, userName, departmentId, email) => {
    set({ loading: true, error: null });
    try {
      const updated = await enterpriseKeysApi.updateKeyBinding(apiKey, { userName, departmentId, email });
      set((state) => ({
        keyBindings: state.keyBindings.map((item) => (item.apiKey === apiKey ? updated : item)),
        loading: false,
      }));
      return updated;
    } catch (error) {
      const message = toMessage(error, 'Failed to update key binding');
      set({ loading: false, error: message });
      throw error;
    }
  },

  deleteKeyBinding: async (apiKey) => {
    set({ loading: true, error: null });
    try {
      await enterpriseKeysApi.deleteKeyBinding(apiKey);
      set((state) => ({
        keyBindings: state.keyBindings.filter((item) => item.apiKey !== apiKey),
        selectedApiKeys: state.selectedApiKeys.filter((item) => item !== apiKey),
        loading: false,
      }));
    } catch (error) {
      const message = toMessage(error, 'Failed to delete key binding');
      set({ loading: false, error: message });
      throw error;
    }
  },

  deleteKeyBindings: async (apiKeys) => {
    set({ loading: true, error: null });
    try {
      await enterpriseKeysApi.deleteKeyBindings(apiKeys);
      const selectedSet = new Set(apiKeys);
      set((state) => ({
        keyBindings: state.keyBindings.filter((item) => !selectedSet.has(item.apiKey)),
        selectedApiKeys: state.selectedApiKeys.filter((item) => !selectedSet.has(item)),
        loading: false,
      }));
    } catch (error) {
      const message = toMessage(error, 'Failed to delete key bindings');
      set({ loading: false, error: message });
      throw error;
    }
  },

  generatePreview: async (csvContent) => {
    set({ loading: true, error: null });
    try {
      const response = await enterpriseKeysApi.generatePreview(csvContent);
      set({ loading: false });
      return response.items;
    } catch (error) {
      const message = toMessage(error, 'Failed to generate preview');
      set({ loading: false, error: message });
      throw error;
    }
  },

  importKeys: async (items) => {
    set({ loading: true, error: null });
    try {
      await enterpriseKeysApi.importKeys(items);
      const [keyBindingsResponse, importHistoryResponse] = await Promise.all([
        enterpriseKeysApi.listKeyBindings(),
        enterpriseKeysApi.listImportHistory(),
      ]);
      set({
        keyBindings: keyBindingsResponse.items,
        importHistory: importHistoryResponse.items,
        loading: false,
      });
    } catch (error) {
      const message = toMessage(error, 'Failed to import keys');
      set({ loading: false, error: message });
      throw error;
    }
  },

  fetchImportHistory: async (limit) => {
    set({ loading: true, error: null });
    try {
      const response = await enterpriseKeysApi.listImportHistory(limit);
      set({ importHistory: response.items, loading: false });
      return response.items;
    } catch (error) {
      const message = toMessage(error, 'Failed to fetch import history');
      set({ loading: false, error: message });
      throw error;
    }
  },
}));

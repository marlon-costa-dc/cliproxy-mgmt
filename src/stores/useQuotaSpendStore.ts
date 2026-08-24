import { create } from 'zustand';
import type { PauseEntry } from '@/services/api/quotaPause';
import { quotaPauseApi } from '@/services/api/quotaPause';

interface QuotaSpendState {
  pausedKeys: PauseEntry[];
  pausedKeysLoading: boolean;
  pausedKeysError: string | null;

  fetchPausedKeys: () => Promise<void>;
  pauseKey: (keyHash: string, reason: string, expiresInSeconds?: number) => Promise<void>;
  resumeKey: (keyHash: string) => Promise<void>;
}

export const useQuotaSpendStore = create<QuotaSpendState>((set, get) => ({
  pausedKeys: [],
  pausedKeysLoading: false,
  pausedKeysError: null,

  fetchPausedKeys: async () => {
    set({ pausedKeysLoading: true, pausedKeysError: null });
    try {
      const data = await quotaPauseApi.listPaused();
      set({ pausedKeys: data.entries ?? [], pausedKeysLoading: false });
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : 'Failed to fetch paused keys';
      set({ pausedKeysError: message, pausedKeysLoading: false });
    }
  },

  pauseKey: async (keyHash: string, reason: string, expiresInSeconds?: number) => {
    await quotaPauseApi.pauseKey(keyHash, reason, expiresInSeconds);
    await get().fetchPausedKeys();
  },

  resumeKey: async (keyHash: string) => {
    await quotaPauseApi.resumeKey(keyHash);
    await get().fetchPausedKeys();
  },
}));

import { afterAll, afterEach, beforeAll, describe, expect, spyOn, test } from 'bun:test';
import axios from 'axios';
import { apiClient } from '@/services/api/client';
import { useAuthStore } from '@/stores/useAuthStore';
import { useConfigStore } from '@/stores/useConfigStore';

const spies: Array<{ mockRestore(): void }> = [];
const originalFetchConfig = useConfigStore.getState().fetchConfig;
const originalStorage = Object.getOwnPropertyDescriptor(globalThis, 'localStorage');
const originalWindow = Object.getOwnPropertyDescriptor(globalThis, 'window');
const memory = new Map<string, string>();
beforeAll(() => {
  Object.defineProperty(globalThis, 'localStorage', {
    configurable: true,
    value: {
      getItem: (key: string) => memory.get(key) ?? null,
      setItem: (key: string, value: string) => memory.set(key, value),
      removeItem: (key: string) => memory.delete(key),
    },
  });
  Object.defineProperty(globalThis, 'window', {
    configurable: true,
    value: { location: { host: 'proxy.invalid' } },
  });
});
afterAll(() => {
  if (originalStorage) Object.defineProperty(globalThis, 'localStorage', originalStorage);
  else Reflect.deleteProperty(globalThis, 'localStorage');
  if (originalWindow) Object.defineProperty(globalThis, 'window', originalWindow);
  else Reflect.deleteProperty(globalThis, 'window');
});
afterEach(() => {
  spies.splice(0).forEach((spy) => spy.mockRestore());
  // Zustand replaces its state object; mockRestore alone can leave the mocked action in new states.
  useConfigStore.setState({ fetchConfig: originalFetchConfig });
  apiClient.setConfig({ apiBase: '', managementKey: '' });
  useAuthStore.setState({ isAuthenticated: false, connectionStatus: 'disconnected' });
});

const credentials = {
  apiBase: 'https://proxy.invalid/gateway',
  managementKey: 'fixture-only',
  rememberPassword: false,
};

describe('v8-only management login', () => {
  test.each([401, 404, 503])('does not resend credentials after v8 HTTP %i', async (status) => {
    const error = Object.assign(new Error('v8 request failed'), { status });
    const fetch = spyOn(useConfigStore.getState(), 'fetchConfig').mockRejectedValue(error);
    const fallback = spyOn(axios, 'get');
    spies.push(fetch, fallback);

    await expect(useAuthStore.getState().login(credentials)).rejects.toBe(error);
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(fallback).not.toHaveBeenCalled();
    expect(useAuthStore.getState().isAuthenticated).toBe(false);
  });
});

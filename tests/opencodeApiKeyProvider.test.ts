import { afterEach, describe, expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { getProviderUsageKey } from '@/components/providers/utils';
import { getProviderKeyCounts } from '@/features/dashboard/hooks/useDashboardOverview';
import { providerLabel } from '@/features/dashboard/utils';
import { opencodeGoToResource, opencodeToResource } from '@/features/providers/adapters';
import { PROVIDER_LOGOS } from '@/features/providers/brandLogos';
import {
  PROVIDER_BRAND_ORDER,
  PROVIDER_DEFAULT_BASE_URLS,
  PROVIDER_DESCRIPTORS,
} from '@/features/providers/descriptors';
import { MODEL_DISCOVERY_BRANDS } from '@/features/providers/sheets/forms/useModelDiscovery';
import { buildProviderGroups } from '@/features/providers/useProviderWorkbench';
import { apiClient } from '@/services/api/client';
import { providersApi } from '@/services/api/providers';
import { normalizeConfigResponse } from '@/services/api/transformers';

const originalGet = apiClient.get;
const originalPut = apiClient.put;
afterEach(() => {
  apiClient.get = originalGet;
  apiClient.put = originalPut;
});

function backend(family: 'opencode' | 'opencode-go', groups: Record<string, unknown>[]) {
  let state = structuredClone(groups);
  apiClient.get = (async (url: string) => {
    expect(url).toBe('/config');
    return { 'api-keys': { [family]: structuredClone(state) } };
  }) as typeof apiClient.get;
  apiClient.put = (async (url: string, data: unknown) => {
    expect(url).toBe(`/config/api-keys/${family}`);
    state = structuredClone(data as Record<string, unknown>[]);
  }) as typeof apiClient.put;
  return () => state;
}

describe('OpenCode Zen and Go API key providers', () => {
  test('normalizes the v8 contract, including auth_index, into dedicated workbench resources', () => {
    const config = normalizeConfigResponse({
      'api-keys': {
        opencode: [
          {
            name: 'opencode-1',
            keys: [
              {
                'api-key': 'zen-secret-a',
                priority: 4,
                weight: 2,
                prefix: 'zen',
                models: [{ name: 'big-pickle', alias: 'zen-pickle' }],
                'excluded-models': ['grok-code'],
                auth_index: 'auth-zen-a',
              },
              { 'api-key': 'zen-secret-b', auth_index: 'auth-zen-b' },
            ],
          },
        ],
        'opencode-go': [
          {
            name: 'opencode-go-1',
            'base-url': 'https://opencode.ai/zen/go',
            keys: [{ 'api-key': 'go-secret-a', 'disable-cooling': true }],
          },
        ],
      },
    });

    expect(config.opencodeApiKeys).toMatchObject([
      {
        apiKey: 'zen-secret-a',
        priority: 4,
        weight: 2,
        prefix: 'zen',
        models: [{ name: 'big-pickle', alias: 'zen-pickle' }],
        excludedModels: ['grok-code'],
        authIndex: 'auth-zen-a',
      },
      { apiKey: 'zen-secret-b', authIndex: 'auth-zen-b' },
    ]);
    expect(config.opencodeGoApiKeys).toMatchObject([
      { apiKey: 'go-secret-a', baseUrl: 'https://opencode.ai/zen/go', disableCooling: true },
    ]);

    const zen = opencodeToResource(config.opencodeApiKeys![0], 0);
    expect(zen.brand).toBe('opencode');
    expect(zen.apiKeyPreview).not.toContain('zen-secret-a');
    expect(zen.authIndex).toBe('auth-zen-a');
    expect(zen.models).toEqual(['big-pickle']);
    expect(zen.baseUrl).toBeNull();
    expect(zen.selector).toEqual({ brand: 'opencode', apiKey: 'zen-secret-a', index: 0 });

    const go = opencodeGoToResource(config.opencodeGoApiKeys![0], 0);
    expect(go.selector).toEqual({
      brand: 'opencodeGo',
      apiKey: 'go-secret-a',
      baseUrl: 'https://opencode.ai/zen/go',
      index: 0,
    });

    const groups = buildProviderGroups(config);
    expect(groups.find((group) => group.id === 'opencode')?.resources).toHaveLength(2);
    expect(groups.find((group) => group.id === 'opencodeGo')?.resources).toHaveLength(1);
    expect(getProviderKeyCounts(config)).toMatchObject({ opencode: 2, 'opencode-go': 1 });
  });

  test('declares workbench capabilities, default gateways, status keys and labels', () => {
    for (const brand of ['opencode', 'opencodeGo'] as const) {
      expect(PROVIDER_DESCRIPTORS[brand].baseUrlRequired).toBe(false);
      expect(PROVIDER_DESCRIPTORS[brand].supportsModels).toBe(true);
      expect(PROVIDER_DESCRIPTORS[brand].supportsWebsockets).toBe(false);
      expect(PROVIDER_DESCRIPTORS[brand].supportsTestModel).toBe(false);
      expect(PROVIDER_LOGOS[brand].src).toBeTruthy();
      expect(MODEL_DISCOVERY_BRANDS).not.toContain(brand);
    }
    expect(PROVIDER_BRAND_ORDER.indexOf('opencode')).toBe(PROVIDER_BRAND_ORDER.indexOf('xai') + 1);
    expect(PROVIDER_BRAND_ORDER.indexOf('opencodeGo')).toBe(
      PROVIDER_BRAND_ORDER.indexOf('opencode') + 1
    );
    expect(PROVIDER_DEFAULT_BASE_URLS.opencode).toBe('https://opencode.ai/zen');
    expect(PROVIDER_DEFAULT_BASE_URLS.opencodeGo).toBe('https://opencode.ai/zen/go');
    expect(getProviderUsageKey('opencode')).toBe('opencode');
    expect(getProviderUsageKey('opencodeGo')).toBe('opencode-go');
    expect(providerLabel('opencode', 'Unknown')).toBe('OpenCode Zen');
    expect(providerLabel('opencode-go', 'Unknown')).toBe('OpenCode Go');
    for (const locale of ['en', 'ko', 'ru', 'tr', 'vi', 'zh-CN', 'zh-TW']) {
      const translations = JSON.parse(readFileSync(`src/i18n/locales/${locale}.json`, 'utf8'));
      expect(translations.providersPage.providerNames.opencode).toBe('OpenCode Zen');
      expect(translations.providersPage.providerNames.opencodeGo).toBe('OpenCode Go');
    }
  });

  test('adds, edits and deletes keys through the v8 family resources', async () => {
    const zen = backend('opencode', [
      { name: 'opencode-1', keys: [{ 'api-key': 'zen-a', auth_index: 'auth-zen-a' }] },
    ]);
    await providersApi.createOpenCodeConfig({ apiKey: 'zen-b', priority: 2 });
    expect(zen()).toEqual([
      { name: 'opencode-1', keys: [{ 'api-key': 'zen-a', auth_index: 'auth-zen-a' }] },
      { name: 'opencode-2', keys: [{ 'api-key': 'zen-b', priority: 2 }] },
    ]);

    const [first] = normalizeConfigResponse({ 'api-keys': { opencode: zen() } }).opencodeApiKeys!;
    await providersApi.updateOpenCodeConfig(first.apiKey, first.baseUrl, { ...first, weight: 5 });
    expect((zen()[0].keys as unknown[])[0]).toEqual({ 'api-key': 'zen-a', weight: 5 });

    const go = backend('opencode-go', []);
    await providersApi.createOpenCodeGoConfig({ apiKey: 'go-a' });
    expect(go()).toEqual([{ name: 'opencode-go-1', keys: [{ 'api-key': 'go-a' }] }]);
    const [goRow] = normalizeConfigResponse({
      'api-keys': { 'opencode-go': go() },
    }).opencodeGoApiKeys!;
    await providersApi.deleteOpenCodeGoConfig(goRow.apiKey, goRow.baseUrl, goRow.source);
    expect(go()).toEqual([{ name: 'opencode-go-1', keys: [] }]);
  });
});

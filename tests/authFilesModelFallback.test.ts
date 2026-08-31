import { afterEach, describe, expect, test } from 'bun:test';
import { authFilesApi } from '../src/services/api/authFiles';
import { apiClient } from '../src/services/api/client';

const originalGet = apiClient.get;

afterEach(() => {
  apiClient.get = originalGet;
});

describe('auth-file model fallback', () => {
  test('merges dynamic models, removes duplicate files and model ids', async () => {
    const requested: string[] = [];
    apiClient.get = (async (url: string) => {
      requested.push(url);
      if (url.includes('first.json')) {
        return { models: [{ id: 'kiro/auto' }, { id: 'kiro/claude-sonnet' }] };
      }
      return {
        models: [
          { id: 'kiro/auto', display_name: 'Duplicate' },
          { id: 'kiro/claude-haiku' },
        ],
      };
    }) as typeof apiClient.get;

    const models = await authFilesApi.getModelsForAuthFiles([
      'first.json',
      'first.json',
      'second.json',
    ]);

    expect(requested).toHaveLength(2);
    expect(models.map((model) => model.id)).toEqual([
      'kiro/auto',
      'kiro/claude-sonnet',
      'kiro/claude-haiku',
    ]);
  });

  test('keeps models from healthy credentials when another lookup fails', async () => {
    apiClient.get = (async (url: string) => {
      if (url.includes('broken.json')) throw new Error('credential unavailable');
      return { models: [{ id: 'kiro/auto' }] };
    }) as typeof apiClient.get;

    const models = await authFilesApi.getModelsForAuthFiles(['broken.json', 'healthy.json']);

    expect(models).toEqual([{ id: 'kiro/auto' }]);
  });

  test('merges static and credential models for a plugin provider', async () => {
    const requested: string[] = [];
    apiClient.get = (async (url: string) => {
      requested.push(url);
      if (url === '/model-definitions/kiro') {
        return { models: [{ id: 'claude-sonnet-5', display_name: 'Static Sonnet' }] };
      }
      if (url.includes('builder.json')) {
        return {
          models: [
            { id: 'claude-sonnet-5', display_name: 'Live Sonnet' },
            { id: 'glm-5' },
          ],
        };
      }
      return { models: [{ id: 'deepseek-3.2' }] };
    }) as typeof apiClient.get;

    const models = await authFilesApi.getModelsForProvider('KIRO', [
      { name: 'builder.json', type: 'kiro' },
      { name: 'other.json', provider: 'kiro' },
      { name: 'claude.json', type: 'claude' },
    ]);

    expect(requested).toContain('/model-definitions/kiro');
    expect(requested.some((url) => url.includes('builder.json'))).toBe(true);
    expect(requested.some((url) => url.includes('other.json'))).toBe(true);
    expect(requested.some((url) => url.includes('claude.json'))).toBe(false);
    expect(models.map((model) => model.id)).toEqual([
      'claude-sonnet-5',
      'deepseek-3.2',
      'glm-5',
    ]);
  });

  test('uses credential models when static definitions reject the provider', async () => {
    apiClient.get = (async (url: string) => {
      if (url === '/model-definitions/kiro') throw Object.assign(new Error('not found'), { status: 404 });
      return { models: [{ id: 'kiro/auto' }] };
    }) as typeof apiClient.get;

    const models = await authFilesApi.getModelsForProvider('kiro', [
      { name: 'builder.json', provider: 'kiro' },
    ]);

    expect(models).toEqual([{ id: 'kiro/auto' }]);
  });
});

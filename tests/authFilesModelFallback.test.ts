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
});

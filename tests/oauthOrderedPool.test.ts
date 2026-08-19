import { describe, expect, test } from 'bun:test';
import { normalizeOauthModelAlias } from '../src/services/api/authFiles';

describe('OAuth ordered alias pools', () => {
  test('preserves repeated aliases with different upstreams in config order', () => {
    const normalized = normalizeOauthModelAlias({
      'oauth-model-alias': {
        codex: [
          { name: 'gpt-5', alias: 'g5' },
          { name: 'gpt-5-mini', alias: 'g5' },
          { name: 'gpt-5', alias: 'g5' },
          { name: 'gpt-5-nano', alias: 'g5' },
        ],
      },
    });

    expect(normalized.codex).toEqual([
      { name: 'gpt-5', alias: 'g5' },
      { name: 'gpt-5-mini', alias: 'g5' },
      { name: 'gpt-5-nano', alias: 'g5' },
    ]);
  });
});

import { describe, expect, it } from 'vitest';
import { resolveCodexChatgptAccountId } from './resolvers';

describe('resolveCodexChatgptAccountId', () => {
  it('falls back to legacy top-level account field', () => {
    expect(
      resolveCodexChatgptAccountId({
        account: 'legacy-account-id',
      } as never)
    ).toBe('legacy-account-id');
  });
});

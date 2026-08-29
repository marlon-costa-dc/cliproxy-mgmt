import { describe, expect, test } from 'bun:test';
import {
  buildDeepSeekQuotaFiles,
  parseDeepSeekBalance,
} from '@/features/quota/providers/deepseek/data';
import type { OpenAIProviderConfig } from '@/types';

describe('DeepSeek balance integration', () => {
  test('creates safe virtual balance cards only for api.deepseek.com keys', () => {
    const providers: OpenAIProviderConfig[] = [
      {
        name: 'deepseek',
        baseUrl: 'https://api.deepseek.com',
        apiKeyEntries: [{ apiKey: 'must-not-leak', authIndex: 'deepseek-key' }],
      },
      {
        name: 'lookalike',
        baseUrl: 'https://api.deepseek.com.example.org',
        apiKeyEntries: [{ apiKey: 'ignored', authIndex: 'ignored-key' }],
      },
    ];

    const files = buildDeepSeekQuotaFiles(providers);
    expect(files).toEqual([
      expect.objectContaining({
        name: 'DeepSeek Balance',
        provider: 'deepseek',
        authIndex: 'deepseek-key',
        runtimeOnly: true,
      }),
    ]);
    expect(JSON.stringify(files)).not.toContain('must-not-leak');
  });

  test('parses the official balance response', () => {
    expect(
      parseDeepSeekBalance({
        is_available: true,
        balance_infos: [
          {
            currency: 'USD',
            total_balance: '1.63',
            granted_balance: '0.00',
            topped_up_balance: '1.63',
          },
        ],
      })
    ).toEqual({
      isAvailable: true,
      balances: [
        {
          currency: 'USD',
          totalBalance: 1.63,
          grantedBalance: 0,
          toppedUpBalance: 1.63,
        },
      ],
    });
  });

  test('rejects malformed or empty balance responses', () => {
    expect(parseDeepSeekBalance({ is_available: true })).toBeNull();
    expect(parseDeepSeekBalance({ is_available: true, balance_infos: [] })).toBeNull();
    expect(
      parseDeepSeekBalance({
        is_available: true,
        balance_infos: [{ currency: 'USD', total_balance: 'invalid' }],
      })
    ).toBeNull();
  });
});

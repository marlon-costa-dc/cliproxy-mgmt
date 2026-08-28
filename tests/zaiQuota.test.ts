import { describe, expect, test } from 'bun:test';
import {
  buildZaiQuotaFiles,
  buildZaiQuotaRows,
  parseZaiQuotaPayload,
} from '@/features/quota/providers/zai/data';
import type { OpenAIProviderConfig } from '@/types';

describe('Z.ai quota integration', () => {
  test('creates safe virtual quota cards only for api.z.ai keys', () => {
    const providers: OpenAIProviderConfig[] = [
      {
        name: 'zai',
        baseUrl: 'https://api.z.ai/api/coding/paas/v4',
        apiKeyEntries: [{ apiKey: 'must-not-leak', authIndex: 'openai-compatibility:zai:0' }],
      },
      {
        name: 'untrusted',
        baseUrl: 'https://example.com/v1',
        apiKeyEntries: [{ apiKey: 'ignored', authIndex: 'other:0' }],
      },
    ];

    const files = buildZaiQuotaFiles(providers);
    expect(files).toHaveLength(1);
    expect(files[0]).toMatchObject({
      name: 'Z.ai Coding Plan',
      provider: 'zai',
      authIndex: 'openai-compatibility:zai:0',
      runtimeOnly: true,
    });
    expect(JSON.stringify(files)).not.toContain('must-not-leak');
  });

  test('maps official 5-hour and weekly Coding Plan limits', () => {
    const payload = parseZaiQuotaPayload({
      success: true,
      data: {
        level: 'max',
        limits: [
          {
            type: 'CREDIT_LIMIT',
            unit: 3,
            number: 5,
            usage: 28000,
            currentValue: 1301,
            remaining: 26698,
            percentage: 4,
            nextResetTime: 1787973214705,
          },
          {
            type: 'CREDIT_LIMIT',
            unit: 6,
            number: 1,
            usage: 140000,
            currentValue: 91460,
            remaining: 48539,
            percentage: 65,
            nextResetTime: 1788188867999,
          },
        ],
      },
    });

    expect(payload).not.toBeNull();
    expect(buildZaiQuotaRows(payload!)).toEqual([
      expect.objectContaining({
        labelKey: 'zai_quota.five_hour',
        usedPercent: 4,
        periodHours: 5,
        resetAtMs: 1787973214705,
      }),
      expect.objectContaining({
        labelKey: 'zai_quota.weekly',
        usedPercent: 65,
        periodHours: 168,
        resetAtMs: 1788188867999,
      }),
    ]);
  });

  test('rejects non-success and malformed payloads', () => {
    expect(parseZaiQuotaPayload({ success: false, data: { limits: [] } })).toBeNull();
    expect(parseZaiQuotaPayload({ success: true, data: {} })).toBeNull();
  });
});

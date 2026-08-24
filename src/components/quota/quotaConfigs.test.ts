import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { TFunction } from 'i18next';
import type { ApiCallResult } from '@/services/api';
import type { AuthFileItem, CodexUsagePayload } from '@/types';

const { requestCodexUsageRawMock, apiCallRequestMock } = vi.hoisted(() => ({
  requestCodexUsageRawMock: vi.fn(),
  apiCallRequestMock: vi.fn(),
}));

vi.mock('@/services/api', () => ({
  apiCallApi: {
    request: apiCallRequestMock,
  },
  authFilesApi: {},
  getApiCallErrorMessage: (result: ApiCallResult) => {
    const body = result.body as Record<string, unknown> | string | null;
    const message =
      body && typeof body === 'object' && typeof body.error === 'string'
        ? body.error
        : typeof body === 'string'
          ? body
          : result.bodyText;

    if (result.statusCode && message) return `${result.statusCode} ${message}`.trim();
    if (result.statusCode) return `HTTP ${result.statusCode}`;
    return message || 'Request failed';
  },
  requestCodexUsageRaw: requestCodexUsageRawMock,
}));

import { CODEX_CONFIG } from './quotaConfigs';

const t = ((key: string, params?: Record<string, unknown>) => {
  if (key === 'codex_quota.reset_credits_expiry_failed') {
    return `reset expiry failed: ${String(params?.message ?? '')}`;
  }
  if (key === 'common.unknown_error') return 'Unknown error';
  return key;
}) as TFunction;

const createUsagePayload = (availableCount: number): CodexUsagePayload => ({
  rate_limit: {
    primary_window: {
      used_percent: 25,
      limit_window_seconds: 18000,
      reset_after_seconds: 300,
    },
  },
  rate_limit_reset_credits: {
    available_count: availableCount,
  },
});

const file: AuthFileItem = {
  name: 'codex.json',
  type: 'codex',
  authIndex: '1',
};

describe('CODEX_CONFIG.fetchQuota reset credit detail errors', () => {
  beforeEach(() => {
    requestCodexUsageRawMock.mockReset();
    apiCallRequestMock.mockReset();
  });

  it('keeps no error when usage reports zero reset credits', async () => {
    requestCodexUsageRawMock.mockResolvedValue({
      result: { statusCode: 200, body: createUsagePayload(0), bodyText: '', hasStatusCode: true, header: {} },
      payload: createUsagePayload(0),
    });

    const quota = await CODEX_CONFIG.fetchQuota(file, t);

    expect(apiCallRequestMock).not.toHaveBeenCalled();
    expect(quota.rateLimitResetCreditsAvailableCount).toBe(0);
    expect(quota.rateLimitResetCreditsError).toBe('');
    expect(quota.rateLimitResetCredits).toEqual([]);
  });

  it('preserves detail fetch errors when usage reports positive reset credits', async () => {
    requestCodexUsageRawMock.mockResolvedValue({
      result: { statusCode: 200, body: createUsagePayload(3), bodyText: '', hasStatusCode: true, header: {} },
      payload: createUsagePayload(3),
    });
    apiCallRequestMock.mockResolvedValue({
      statusCode: 502,
      hasStatusCode: true,
      header: { 'Content-Type': ['application/json'] },
      bodyText: '{"error":"request failed"}',
      body: { error: 'request failed' },
    });

    const quota = await CODEX_CONFIG.fetchQuota(file, t);

    expect(apiCallRequestMock).toHaveBeenCalledWith(
      expect.objectContaining({
        url: 'https://chatgpt.com/backend-api/wham/rate-limit-reset-credits',
      }),
      expect.anything()
    );
    expect(quota.rateLimitResetCreditsAvailableCount).toBe(3);
    expect(quota.rateLimitResetCredits).toEqual([]);
    expect(quota.rateLimitResetCreditsError).toBe('502 request failed');
  });

  it('clears the error when reset credit details load successfully', async () => {
    requestCodexUsageRawMock.mockResolvedValue({
      result: { statusCode: 200, body: createUsagePayload(2), bodyText: '', hasStatusCode: true, header: {} },
      payload: createUsagePayload(2),
    });
    apiCallRequestMock.mockResolvedValue({
      statusCode: 200,
      hasStatusCode: true,
      header: { 'Content-Type': ['application/json'] },
      bodyText:
        '{"available_count":2,"credits":[{"id":"a","status":"available","reset_type":"codex_rate_limits","granted_at":"2026-07-07T00:00:00Z","expires_at":"2026-07-08T00:00:00Z"}]}',
      body: {
        available_count: 2,
        credits: [
          {
            id: 'a',
            status: 'available',
            reset_type: 'codex_rate_limits',
            granted_at: '2026-07-07T00:00:00Z',
            expires_at: '2026-07-08T00:00:00Z',
          },
        ],
      },
    });

    const quota = await CODEX_CONFIG.fetchQuota(file, t);

    expect(quota.rateLimitResetCreditsAvailableCount).toBe(2);
    expect(quota.rateLimitResetCredits).toHaveLength(1);
    expect(quota.rateLimitResetCreditsError).toBe('');
  });
});

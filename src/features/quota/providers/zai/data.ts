import type { TFunction } from 'i18next';
import type {
  AuthFileItem,
  OpenAIProviderConfig,
  ZaiQuotaPayload,
  ZaiQuotaRow,
  ZaiQuotaState,
} from '@/types';
import { apiCallApi, getApiCallErrorMessage } from '@/services/api';
import { normalizeAuthIndex } from '@/utils/authIndex';
import type { QuotaProviderData } from '../types';

export const ZAI_QUOTA_URL = 'https://api.z.ai/api/monitor/usage/quota/limit';
export const ZAI_REQUEST_HEADERS = {
  Authorization: '$TOKEN$',
  Accept: 'application/json',
  'Accept-Language': 'en-US,en',
};

const numberValue = (value: unknown): number | null => {
  const parsed = typeof value === 'number' ? value : Number(value);
  return Number.isFinite(parsed) ? parsed : null;
};

const periodFor = (unit: number | null, count: number | null): number | null => {
  if (unit === 3 && count === 5) return 5;
  if (unit === 6 && count === 1) return 24 * 7;
  return null;
};

export function parseZaiQuotaPayload(value: unknown): ZaiQuotaPayload | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null;
  const payload = value as ZaiQuotaPayload;
  if (payload.success !== true || !Array.isArray(payload.data?.limits)) return null;
  return payload;
}

export function buildZaiQuotaRows(payload: ZaiQuotaPayload): ZaiQuotaRow[] {
  return (payload.data?.limits ?? []).flatMap((limit, index) => {
    if (limit.type !== 'CREDIT_LIMIT') return [];
    const maximum = numberValue(limit.usage);
    const used = numberValue(limit.currentValue);
    const remaining = numberValue(limit.remaining);
    if (maximum === null || used === null || remaining === null || maximum <= 0) return [];

    const unit = numberValue(limit.unit);
    const count = numberValue(limit.number);
    const periodHours = periodFor(unit, count);
    const usedPercentValue = numberValue(limit.percentage);
    const usedPercent = Math.min(
      100,
      Math.max(0, usedPercentValue ?? Math.round((used / maximum) * 100))
    );
    const resetAt = numberValue(limit.nextResetTime);
    const labelKey =
      periodHours === 5
        ? 'zai_quota.five_hour'
        : periodHours === 168
          ? 'zai_quota.weekly'
          : 'zai_quota.credit_limit';

    return [
      {
        id: `${unit ?? 'unknown'}:${count ?? index}`,
        labelKey,
        used,
        limit: maximum,
        remaining,
        usedPercent,
        resetAtMs: resetAt !== null && resetAt > 0 ? resetAt : null,
        periodHours,
      },
    ];
  });
}

export function buildZaiQuotaFiles(providers: OpenAIProviderConfig[]): AuthFileItem[] {
  const matches = providers.filter((provider) => {
    try {
      return new URL(provider.baseUrl).hostname.toLowerCase() === 'api.z.ai';
    } catch {
      return false;
    }
  });
  const totalKeys = matches.reduce((sum, provider) => sum + provider.apiKeyEntries.length, 0);
  let ordinal = 0;

  return matches.flatMap((provider) =>
    provider.apiKeyEntries.flatMap((entry) => {
      ordinal += 1;
      const authIndex = normalizeAuthIndex(entry.authIndex);
      if (!authIndex) return [];
      return [
        {
          name: totalKeys === 1 ? 'Z.ai Coding Plan' : `Z.ai Coding Plan #${ordinal}`,
          type: 'zai',
          provider: 'zai',
          authIndex,
          runtimeOnly: true,
          disabled: provider.disabled === true,
        } satisfies AuthFileItem,
      ];
    })
  );
}

const fetchZaiQuota = async (
  file: AuthFileItem,
  t: TFunction
): Promise<{ rows: ZaiQuotaRow[]; planType: string | null }> => {
  const authIndex = normalizeAuthIndex(file['auth_index'] ?? file.authIndex);
  if (!authIndex) throw new Error(t('zai_quota.missing_auth_index'));

  const result = await apiCallApi.request({
    authIndex,
    method: 'GET',
    url: ZAI_QUOTA_URL,
    header: { ...ZAI_REQUEST_HEADERS },
  });
  if (result.statusCode < 200 || result.statusCode >= 300) {
    const error = new Error(getApiCallErrorMessage(result)) as Error & { status?: number };
    error.status = result.statusCode;
    throw error;
  }

  const payload = parseZaiQuotaPayload(result.body);
  if (!payload) throw new Error(t('zai_quota.empty_data'));
  const rows = buildZaiQuotaRows(payload);
  if (rows.length === 0) throw new Error(t('zai_quota.empty_data'));
  return { rows, planType: payload.data?.level ?? null };
};

export const ZAI_CONFIG: QuotaProviderData<
  ZaiQuotaState,
  { rows: ZaiQuotaRow[]; planType: string | null }
> = {
  type: 'zai',
  i18nPrefix: 'zai_quota',
  filterFn: (file) => file.provider === 'zai' && file.disabled !== true,
  fetchQuota: fetchZaiQuota,
  storeSelector: (state) => state.zaiQuota,
  storeSetter: 'setZaiQuota',
  buildLoadingState: () => ({ status: 'loading', rows: [] }),
  buildSuccessState: ({ rows, planType }) => ({ status: 'success', rows, planType }),
  buildErrorState: (message, status) => ({
    status: 'error',
    rows: [],
    error: message,
    errorStatus: status,
  }),
};

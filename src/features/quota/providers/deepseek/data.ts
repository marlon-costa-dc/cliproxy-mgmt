import type { TFunction } from 'i18next';
import type {
  AuthFileItem,
  DeepSeekBalanceInfo,
  DeepSeekBalancePayload,
  DeepSeekQuotaState,
  OpenAIProviderConfig,
} from '@/types';
import { apiCallApi, getApiCallErrorMessage } from '@/services/api';
import { normalizeAuthIndex } from '@/utils/authIndex';
import type { QuotaProviderData } from '../types';

export const DEEPSEEK_BALANCE_URL = 'https://api.deepseek.com/user/balance';
export const DEEPSEEK_REQUEST_HEADERS = {
  Authorization: 'Bearer $TOKEN$',
  Accept: 'application/json',
};

const amount = (value: unknown): number | null => {
  const parsed = typeof value === 'number' ? value : Number(value);
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : null;
};

export function parseDeepSeekBalance(value: unknown): {
  isAvailable: boolean;
  balances: DeepSeekBalanceInfo[];
} | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null;
  const payload = value as DeepSeekBalancePayload;
  if (typeof payload.is_available !== 'boolean' || !Array.isArray(payload.balance_infos)) {
    return null;
  }

  const balances = payload.balance_infos.flatMap((entry) => {
    const currency = String(entry.currency ?? '').trim().toUpperCase();
    const totalBalance = amount(entry.total_balance);
    const grantedBalance = amount(entry.granted_balance);
    const toppedUpBalance = amount(entry.topped_up_balance);
    if (!currency || totalBalance === null || grantedBalance === null || toppedUpBalance === null) {
      return [];
    }
    return [{ currency, totalBalance, grantedBalance, toppedUpBalance }];
  });
  if (balances.length === 0) return null;
  return { isAvailable: payload.is_available, balances };
}

export function buildDeepSeekQuotaFiles(providers: OpenAIProviderConfig[]): AuthFileItem[] {
  const matches = providers.filter((provider) => {
    try {
      return new URL(provider.baseUrl).hostname.toLowerCase() === 'api.deepseek.com';
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
          name: totalKeys === 1 ? 'DeepSeek Balance' : `DeepSeek Balance #${ordinal}`,
          type: 'deepseek',
          provider: 'deepseek',
          authIndex,
          runtimeOnly: true,
          disabled: provider.disabled === true,
        } satisfies AuthFileItem,
      ];
    })
  );
}

const fetchDeepSeekBalance = async (
  file: AuthFileItem,
  t: TFunction
): Promise<{ isAvailable: boolean; balances: DeepSeekBalanceInfo[] }> => {
  const authIndex = normalizeAuthIndex(file['auth_index'] ?? file.authIndex);
  if (!authIndex) throw new Error(t('deepseek_quota.missing_auth_index'));

  const result = await apiCallApi.request({
    authIndex,
    method: 'GET',
    url: DEEPSEEK_BALANCE_URL,
    header: { ...DEEPSEEK_REQUEST_HEADERS },
  });
  if (result.statusCode < 200 || result.statusCode >= 300) {
    const error = new Error(getApiCallErrorMessage(result)) as Error & { status?: number };
    error.status = result.statusCode;
    throw error;
  }

  const balance = parseDeepSeekBalance(result.body);
  if (!balance) throw new Error(t('deepseek_quota.empty_data'));
  return balance;
};

export const DEEPSEEK_CONFIG: QuotaProviderData<
  DeepSeekQuotaState,
  { isAvailable: boolean; balances: DeepSeekBalanceInfo[] }
> = {
  type: 'deepseek',
  i18nPrefix: 'deepseek_quota',
  filterFn: (file) => file.provider === 'deepseek' && file.disabled !== true,
  fetchQuota: fetchDeepSeekBalance,
  storeSelector: (state) => state.deepseekQuota,
  storeSetter: 'setDeepseekQuota',
  buildLoadingState: () => ({ status: 'loading', isAvailable: null, balances: [] }),
  buildSuccessState: ({ isAvailable, balances }) => ({
    status: 'success',
    isAvailable,
    balances,
  }),
  buildErrorState: (message, status) => ({
    status: 'error',
    isAvailable: null,
    balances: [],
    error: message,
    errorStatus: status,
  }),
};

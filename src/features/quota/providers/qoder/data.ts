/** Qoder quota data comes from the auth-file snapshot populated by the backend. */

import type { TFunction } from 'i18next';
import type { AuthFileItem, QoderQuotaState, QoderUsageSnapshot } from '@/types';
import { isDisabledAuthFile, isQoderFile, normalizeNumberValue } from '@/utils/quota';
import type { QuotaProviderData } from '../types';

export const readQoderUsageSnapshot = (file: AuthFileItem): QoderUsageSnapshot | null => {
  const raw = file.usage;
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return null;
  return raw as QoderUsageSnapshot;
};

export const qoderRemainingPercent = (usage: QoderUsageSnapshot): number | null => {
  const explicit = normalizeNumberValue(usage.percentage);
  if (explicit !== null) {
    const used = explicit <= 1 ? explicit * 100 : explicit;
    return 100 - Math.max(0, Math.min(100, used));
  }
  const total = normalizeNumberValue(usage.total);
  if (!total || total <= 0) return null;
  const used = normalizeNumberValue(usage.used);
  if (used !== null) return 100 - Math.max(0, Math.min(100, (used / total) * 100));
  const remaining = normalizeNumberValue(usage.remaining);
  return remaining === null ? null : Math.max(0, Math.min(100, (remaining / total) * 100));
};

const fetchQoderQuota = async (file: AuthFileItem, t: TFunction): Promise<QoderUsageSnapshot> => {
  const usage = readQoderUsageSnapshot(file);
  if (!usage) throw new Error(t('qoder_quota.empty_data'));
  return usage;
};

export const QODER_CONFIG: QuotaProviderData<QoderQuotaState, QoderUsageSnapshot> = {
  type: 'qoder',
  i18nPrefix: 'qoder_quota',
  filterFn: (file) => isQoderFile(file) && !isDisabledAuthFile(file),
  fetchQuota: fetchQoderQuota,
  storeSelector: (state) => state.qoderQuota,
  storeSetter: 'setQoderQuota',
  buildLoadingState: () => ({ status: 'loading', usage: null }),
  buildSuccessState: (usage) => ({ status: 'success', usage }),
  buildErrorState: (message, status) => ({
    status: 'error',
    usage: null,
    error: message,
    errorStatus: status,
  }),
};

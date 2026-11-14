/** Qoder quota snapshot renderer for both the quota page and auth-file cards. */

import { useTranslation } from 'react-i18next';
import type { QoderQuotaState } from '@/types';
import { normalizeNumberValue, normalizeStringValue } from '@/utils/quota';
import { QuotaMeter } from '../../components/QuotaMeter';
import type { QuotaBodyProps } from '../../types';
import { qoderRemainingPercent } from './data';

const normalizeBoolean = (value: unknown): boolean => {
  if (typeof value === 'boolean') return value;
  if (typeof value === 'number') return value !== 0;
  if (typeof value !== 'string') return false;
  return ['true', '1', 'yes', 'y', 'on'].includes(value.trim().toLowerCase());
};

const formatAmount = (value: number | null, unit = ''): string => {
  if (value === null) return '-';
  const formatted = new Intl.NumberFormat(undefined, { maximumFractionDigits: 2 }).format(value);
  return unit ? `${formatted} ${unit}` : formatted;
};

const formatExpiry = (value: unknown): string | null => {
  const numeric = normalizeNumberValue(value);
  const date =
    numeric !== null && numeric > 0
      ? new Date(numeric < 1e12 ? numeric * 1000 : numeric)
      : typeof value === 'string'
        ? new Date(value)
        : null;
  if (!date || Number.isNaN(date.getTime())) return null;
  return date.toLocaleString(undefined, {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  });
};

export function QoderQuotaBody({ quota, classes }: QuotaBodyProps<QoderQuotaState>) {
  const { t } = useTranslation();
  const usage = quota.usage ?? null;
  if (!usage) return <div className={classes.quotaMessage}>{t('qoder_quota.empty_data')}</div>;

  const unit = normalizeStringValue(usage.unit) ?? t('qoder_quota.unit_default');
  const used = normalizeNumberValue(usage.used);
  const total = normalizeNumberValue(usage.total);
  const remaining = normalizeNumberValue(usage.remaining);
  const orgRemaining = normalizeNumberValue(
    usage.org_resource_remaining ?? usage.orgResourceRemaining
  );
  const remainingPercent = qoderRemainingPercent(usage);
  const expiry = formatExpiry(usage.expires_at ?? usage.expiresAt);
  const amountLabel =
    used !== null && total !== null
      ? `${formatAmount(used)} / ${formatAmount(total, unit)}`
      : remaining !== null
        ? t('qoder_quota.remaining_amount', { amount: formatAmount(remaining, unit) })
        : null;
  const exceeded = normalizeBoolean(usage.is_quota_exceeded ?? usage.isQuotaExceeded);

  return (
    <>
      <div className={classes.quotaRow}>
        <div className={classes.quotaRowHeader}>
          <span className={classes.quotaModel}>{t('qoder_quota.usage_label')}</span>
          <div className={classes.quotaMeta}>
            <span className={classes.quotaPercent}>
              {remainingPercent === null ? '--' : `${Math.round(remainingPercent)}%`}
            </span>
            {amountLabel && <span className={classes.quotaAmount}>{amountLabel}</span>}
            {expiry && (
              <span className={classes.quotaReset}>
                {t('qoder_quota.expires_at', { time: expiry })}
              </span>
            )}
          </div>
        </div>
        <QuotaMeter percent={remainingPercent} classes={classes} />
      </div>
      {orgRemaining !== null && (
        <div className={classes.codexPlan}>
          <span className={classes.codexPlanLabel}>{t('qoder_quota.org_resource_remaining')}</span>
          <span className={classes.codexPlanValue}>{formatAmount(orgRemaining, unit)}</span>
        </div>
      )}
      {exceeded && (
        <div className={classes.quotaWarningMessage} role="alert">
          {t('qoder_quota.quota_exceeded')}
        </div>
      )}
    </>
  );
}

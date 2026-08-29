import { useTranslation } from 'react-i18next';
import type { DeepSeekQuotaState } from '@/types';
import type { QuotaBodyProps } from '../../types';

const formatBalance = (amount: number, currency: string, locale?: string): string => {
  try {
    return new Intl.NumberFormat(locale, {
      style: 'currency',
      currency,
      minimumFractionDigits: 2,
      maximumFractionDigits: 2,
    }).format(amount);
  } catch {
    return `${currency} ${amount.toFixed(2)}`;
  }
};

export function DeepSeekQuotaBody({ quota, classes }: QuotaBodyProps<DeepSeekQuotaState>) {
  const { t, i18n } = useTranslation();
  if (quota.balances.length === 0) {
    return <div className={classes.quotaMessage}>{t('deepseek_quota.empty_data')}</div>;
  }

  return (
    <>
      <div className={classes.codexPlan}>
        <span className={classes.codexPlanLabel}>{t('deepseek_quota.status_label')}</span>
        <span className={classes.codexPlanValue}>
          {t(quota.isAvailable ? 'deepseek_quota.available' : 'deepseek_quota.unavailable')}
        </span>
      </div>
      {quota.balances.map((balance) => (
        <div key={balance.currency} className={classes.quotaRow}>
          <div className={classes.quotaRowHeader}>
            <span className={classes.quotaModel}>
              {t('deepseek_quota.total_balance', { currency: balance.currency })}
            </span>
            <span className={classes.quotaPercent}>
              {formatBalance(balance.totalBalance, balance.currency, i18n.resolvedLanguage)}
            </span>
          </div>
          <div className={classes.quotaMeta}>
            <span className={classes.quotaAmount}>
              {t('deepseek_quota.topped_up', {
                amount: formatBalance(
                  balance.toppedUpBalance,
                  balance.currency,
                  i18n.resolvedLanguage
                ),
              })}
            </span>
            <span className={classes.quotaAmount}>
              {t('deepseek_quota.granted', {
                amount: formatBalance(
                  balance.grantedBalance,
                  balance.currency,
                  i18n.resolvedLanguage
                ),
              })}
            </span>
          </div>
        </div>
      ))}
    </>
  );
}

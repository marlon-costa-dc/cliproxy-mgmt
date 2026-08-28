import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import type { ZaiQuotaState } from '@/types';
import { buildResetDisplay } from '@/utils/quota';
import { useNow } from '@/hooks/useNow';
import { QuotaMeter } from '../../components/QuotaMeter';
import { QuotaResetLabel } from '../../components/QuotaResetLabel';
import { collectQuotaRowInstants, pickUrgentRowId } from '../../resetSchedule';
import type { QuotaBodyProps } from '../../types';

export function ZaiQuotaBody({ quota, classes }: QuotaBodyProps<ZaiQuotaState>) {
  const { t, i18n } = useTranslation();
  const now = useNow();
  const soonestRowId = useMemo(
    () => pickUrgentRowId(collectQuotaRowInstants('zai', quota), now),
    [quota, now]
  );

  if (quota.rows.length === 0) {
    return <div className={classes.quotaMessage}>{t('zai_quota.empty_data')}</div>;
  }

  return (
    <>
      {quota.planType && (
        <div className={classes.codexPlan}>
          <span className={classes.codexPlanLabel}>{t('zai_quota.plan_label')}</span>
          <span className={classes.codexPlanValue}>{quota.planType.toUpperCase()}</span>
        </div>
      )}
      {quota.rows.map((row, index) => {
        const remainingPercent = Math.max(0, Math.min(100, 100 - row.usedPercent));
        const resetDisplay = buildResetDisplay(
          null,
          row.resetAtMs,
          now,
          i18n.resolvedLanguage
        );
        const soon = row.id === soonestRowId;
        return (
          <div
            key={row.id}
            className={classes.quotaRow}
            title={soon ? t('quota_management.soonest_row_hint') : undefined}
          >
            <div className={classes.quotaRowHeader}>
              <span className={classes.quotaModel}>{t(row.labelKey)}</span>
              <div className={classes.quotaMeta}>
                <span className={classes.quotaAmount}>
                  {t('zai_quota.remaining_credits', {
                    remaining: row.remaining.toLocaleString(),
                    total: row.limit.toLocaleString(),
                  })}
                </span>
                <span className={classes.quotaPercent}>{Math.round(remainingPercent)}%</span>
                {resetDisplay && (
                  <QuotaResetLabel display={resetDisplay} classes={classes} soon={soon} />
                )}
              </div>
            </div>
            <QuotaMeter percent={remainingPercent} classes={classes} index={index} />
          </div>
        );
      })}
    </>
  );
}

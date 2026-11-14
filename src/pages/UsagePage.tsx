import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { LoadingSpinner } from '@/components/ui/LoadingSpinner';
import { useHeaderRefresh } from '@/hooks/useHeaderRefresh';
import { apiKeyUsageApi } from '@/services/api';
import type { ApiKeyUsageResponse } from '@/utils/recentRequests';
import { formatLargeNumber, summarizeApiKeyUsage } from '@/utils/usageStats';
import { createLatestUsageRequestGuard, runLatestUsageRequest } from './usage-page-request-guard';
import styles from './UsagePage.module.scss';

function StatCard({ label, value, meta }: { label: string; value: number; meta?: string }) {
  return (
    <div className={styles.statCard}>
      <span className={styles.statLabel}>{label}</span>
      <span className={styles.statValue}>{formatLargeNumber(value)}</span>
      {meta && <span className={styles.statMeta}>{meta}</span>}
    </div>
  );
}

function TrendCard({
  title,
  points,
  emptyText,
}: {
  title: string;
  points: { label: string; value: number }[];
  emptyText: string;
}) {
  const max = Math.max(0, ...points.map((point) => point.value));

  return (
    <Card title={title}>
      {points.length === 0 || max === 0 ? (
        <div className={styles.trendEmpty}>{emptyText}</div>
      ) : (
        <div className={styles.trendBars}>
          {points.map((point) => (
            <div
              className={styles.trendBar}
              key={point.label}
              title={`${point.label}: ${point.value}`}
            >
              <div
                className={styles.trendBarFill}
                style={{ height: `${Math.max(3, (point.value / max) * 100)}%` }}
              />
            </div>
          ))}
        </div>
      )}
    </Card>
  );
}

export function UsagePage() {
  const { t } = useTranslation();
  const [payload, setPayload] = useState<ApiKeyUsageResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null);
  const requestGuard = useRef(createLatestUsageRequestGuard());

  const loadUsage = useCallback(
    () =>
      runLatestUsageRequest({
        guard: requestGuard.current,
        request: () => apiKeyUsageApi.getUsage(),
        fallbackError: t('usage_stats.load_failed'),
        onStart: () => {
          setLoading(true);
          setError(null);
        },
        onSuccess: (nextPayload) => {
          setPayload(nextPayload);
          setLastUpdated(new Date());
        },
        onError: setError,
        onFinish: () => setLoading(false),
      }),
    [t]
  );

  useEffect(() => {
    void loadUsage().catch(() => undefined);
    const guard = requestGuard.current;
    return () => guard.invalidate();
  }, [loadUsage]);

  useHeaderRefresh(loadUsage);

  const summary = useMemo(() => summarizeApiKeyUsage(payload), [payload]);
  const requestPoints = useMemo(
    () =>
      summary.recent.map((bucket, index) => ({
        label: bucket.time ?? String(index),
        value: bucket.success + bucket.failed,
      })),
    [summary]
  );

  return (
    <div className={styles.container}>
      <div className={styles.header}>
        <div>
          <h1 className={styles.pageTitle}>{t('usage_stats.title')}</h1>
          <p className={styles.subtitle}>{t('usage_stats.subtitle')}</p>
        </div>
        <div className={styles.headerActions}>
          {lastUpdated && (
            <span className={styles.lastUpdated}>
              {t('usage_stats.last_updated')}: {lastUpdated.toLocaleTimeString()}
            </span>
          )}
          <Button
            variant="secondary"
            size="sm"
            onClick={() => void loadUsage().catch(() => undefined)}
            loading={loading}
          >
            {t('usage_stats.refresh')}
          </Button>
        </div>
      </div>

      {error && <div className={styles.errorBox}>{error}</div>}

      <div className={styles.statGrid} aria-busy={loading}>
        <StatCard
          label={t('usage_stats.total_requests')}
          value={summary.success + summary.failures}
        />
        <StatCard label={t('usage_stats.success')} value={summary.success} />
        <StatCard label={t('usage_stats.failure')} value={summary.failures} />
      </div>

      {loading && !payload ? (
        <div className={styles.emptyState}>
          <LoadingSpinner />
        </div>
      ) : (
        <>
          <div className={styles.chartGrid}>
            <TrendCard
              title={t('usage_stats.recent_requests')}
              points={requestPoints}
              emptyText={t('usage_stats.no_data')}
            />
          </div>

          <Card title={t('usage_stats.api_breakdown')}>
            {summary.providers.length === 0 ? (
              <div className={styles.emptyState}>{t('usage_stats.no_data')}</div>
            ) : (
              <div className={styles.tableWrap}>
                <table className={styles.table}>
                  <thead>
                    <tr>
                      <th>{t('usage_stats.api')}</th>
                      <th className={styles.numberCell}>{t('usage_stats.credentials')}</th>
                      <th className={styles.numberCell}>{t('usage_stats.success')}</th>
                      <th className={styles.numberCell}>{t('usage_stats.failures')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {summary.providers.map((row) => (
                      <tr key={row.provider}>
                        <td className={styles.mono}>{row.provider}</td>
                        <td className={styles.numberCell}>{formatLargeNumber(row.credentials)}</td>
                        <td className={styles.numberCell}>{formatLargeNumber(row.success)}</td>
                        <td className={styles.numberCell}>{formatLargeNumber(row.failures)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Card>
        </>
      )}
    </div>
  );
}

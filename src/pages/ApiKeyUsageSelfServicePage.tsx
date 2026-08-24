import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { useNotificationStore } from '@/stores';
import type { NotificationType } from '@/types';
import { querySelfUsage, type SelfUsageResponse } from '@/services/api/selfUsage';
import styles from './ApiKeyUsageSelfServicePage.module.scss';

function formatCents(cents: number): string {
  return `$${(cents / 100).toFixed(2)}`;
}

function cacheHitRate(inputTokens: number, outputTokens: number, cacheReadTokens: number): string {
  const total = inputTokens + outputTokens;
  if (total === 0) return '0%';
  return `${((cacheReadTokens / total) * 100).toFixed(1)}%`;
}

export function ApiKeyUsageSelfServicePage() {
  const { t } = useTranslation();
  const { showNotification } = useNotificationStore();
  const proxyOrigin = typeof window !== 'undefined' ? window.location.origin : '';

  const [apiKey, setApiKey] = useState('');
  const [data, setData] = useState<SelfUsageResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [searched, setSearched] = useState(false);

  const handleSearch = async () => {
    if (!apiKey.trim()) {
      showNotification(t('self_usage.key_required'), 'error' as NotificationType);
      return;
    }
    setLoading(true);
    setSearched(false);
    try {
      const result = await querySelfUsage(proxyOrigin, apiKey.trim());
      setData(result);
      setSearched(true);
    } catch (err: unknown) {
      showNotification(err instanceof Error ? err.message : 'Query failed', 'error' as NotificationType);
      setData(null);
      setSearched(true);
    } finally {
      setLoading(false);
    }
  };

  const renderUsagePeriod = (period: SelfUsageResponse['usage']['today'], label: string) => (
    <div className={styles.usageCard}>
      <h3>{label}</h3>
      <div className={styles.metrics}>
        <div className={styles.metric}>
          <span className={styles.metricLabel}>{t('self_usage.requests')}</span>
          <span className={styles.metricValue}>{period.requests}</span>
        </div>
        <div className={styles.metric}>
          <span className={styles.metricLabel}>{t('self_usage.success')}</span>
          <span className={styles.metricValue}>{period.success}</span>
        </div>
        <div className={styles.metric}>
          <span className={styles.metricLabel}>{t('self_usage.failed')}</span>
          <span className={styles.metricValue}>{period.failed}</span>
        </div>
        <div className={styles.metric}>
          <span className={styles.metricLabel}>{t('self_usage.input_tokens')}</span>
          <span className={styles.metricValue}>{period.input_tokens.toLocaleString()}</span>
        </div>
        <div className={styles.metric}>
          <span className={styles.metricLabel}>{t('self_usage.output_tokens')}</span>
          <span className={styles.metricValue}>{period.output_tokens.toLocaleString()}</span>
        </div>
        <div className={styles.metric}>
          <span className={styles.metricLabel}>{t('self_usage.cache_hit_rate')}</span>
          <span className={styles.metricValue}>{cacheHitRate(period.input_tokens, period.output_tokens, period.cache_read_tokens)}</span>
        </div>
        <div className={styles.metric}>
          <span className={styles.metricLabel}>{t('self_usage.total_tokens')}</span>
          <span className={styles.metricValue}>{period.total_tokens.toLocaleString()}</span>
        </div>
        <div className={styles.metric}>
          <span className={styles.metricLabel}>{t('self_usage.cost')}</span>
          <span className={styles.metricValue}>{formatCents(period.cost_cents)}</span>
        </div>
        {period.limit_cents > 0 && (
          <div className={styles.metric}>
            <span className={styles.metricLabel}>{t('self_usage.limit')}</span>
            <span className={styles.metricValue}>{formatCents(period.limit_cents)}</span>
          </div>
        )}
      </div>
      {period.limit_cents > 0 && (
        <div className={styles.progressBar}>
          <div
            className={styles.progressFill}
            style={{ width: `${Math.min((period.cost_cents / period.limit_cents) * 100, 100)}%` }}
          />
        </div>
      )}
    </div>
  );

  const renderModelTable = (byModel: Record<string, SelfUsageResponse['usage']['by_model'][string]>) => {
    const models = Object.entries(byModel);
    if (models.length === 0) return null;
    return (
      <Card className={styles.section}>
        <h3>{t('self_usage.by_model')}</h3>
        <table className={styles.table}>
          <thead>
            <tr>
              <th>{t('self_usage.model')}</th>
              <th>{t('self_usage.requests')}</th>
              <th>{t('self_usage.input_tokens')}</th>
              <th>{t('self_usage.output_tokens')}</th>
              <th>{t('self_usage.cost')}</th>
            </tr>
          </thead>
          <tbody>
            {models.map(([model, usage]) => (
              <tr key={model}>
                <td>{model}</td>
                <td>{usage.requests}</td>
                <td>{usage.input_tokens.toLocaleString()}</td>
                <td>{usage.output_tokens.toLocaleString()}</td>
                <td>{formatCents(usage.cost_cents)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </Card>
    );
  };

  return (
    <div className={styles.container}>
      <div className={styles.header}>
        <h1>{t('self_usage.title')}</h1>
        <p className={styles.description}>{t('self_usage.description')}</p>
      </div>

      <Card className={styles.searchSection}>
        <div className={styles.searchRow}>
          <Input
            placeholder={t('self_usage.api_key_placeholder')}
            value={apiKey}
            onChange={(e) => setApiKey(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && handleSearch()}
          />
          <Button onClick={handleSearch} disabled={loading}>
            {loading ? t('common.loading') : t('self_usage.query')}
          </Button>
        </div>
      </Card>

      {data && (
        <>
          <Card className={styles.statusCard}>
            <div className={styles.statusRow}>
              <span className={styles.statusLabel}>{t('self_usage.pause_status')}</span>
              <span className={data.quota.paused ? styles.statusPaused : styles.statusActive}>
                {data.quota.paused ? t('self_usage.paused') : t('self_usage.active')}
              </span>
            </div>
            {data.quota.paused && (
              <>
                <div className={styles.statusRow}>
                  <span className={styles.statusLabel}>{t('self_usage.pause_reason')}</span>
                  <span>{data.quota.paused_reason}</span>
                </div>
                {data.quota.resumes_at && (
                  <div className={styles.statusRow}>
                    <span className={styles.statusLabel}>{t('self_usage.resumes_at')}</span>
                    <span>{new Date(data.quota.resumes_at).toLocaleString()}</span>
                  </div>
                )}
              </>
            )}
          </Card>

          {data.usage.available ? (
            <>
              <div className={styles.usageGrid}>
                {renderUsagePeriod(data.usage.today, t('self_usage.today'))}
                {renderUsagePeriod(data.usage.this_week, t('self_usage.this_week'))}
              </div>
              {renderModelTable(data.usage.by_model)}
            </>
          ) : (
            <Card className={styles.section}>
              <p className={styles.unavailable}>{t('self_usage.usage_unavailable')}</p>
            </Card>
          )}
        </>
      )}

      {searched && !data && (
        <Card className={styles.section}>
          <p className={styles.noData}>{t('self_usage.no_data')}</p>
        </Card>
      )}
    </div>
  );
}
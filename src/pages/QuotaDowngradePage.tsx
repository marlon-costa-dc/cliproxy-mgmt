import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { Select } from '@/components/ui/Select';
import { useNotificationStore } from '@/stores';
import type { NotificationType } from '@/types';
import { quotaLimitsApi, type SpendLimitEntry } from '@/services/api/quotaLimits';
import { quotaPauseApi, type DowngradeEntry } from '@/services/api/quotaPause';
import { enterpriseKeysApi } from '@/services/api/enterpriseKeys';
import type { EnterpriseKeyBinding } from '@/types/enterpriseKey';
import { quotaKeyHash } from '@/utils/apiKeyHash';
import styles from './QuotaDowngradePage.module.scss';

const DEFAULT_FALLBACK_MODEL = 'gpt-5.6-luna';

const formatTime = (value: string): string => (value ? new Date(value).toLocaleString() : '-');

const buildBindingLabel = (userName: string, email?: string): string =>
  email ? `${userName} (${email})` : userName;

export function QuotaDowngradePage() {
  const { t } = useTranslation();
  const { showNotification } = useNotificationStore();
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [enabled, setEnabled] = useState(false);
  const [dailyCents, setDailyCents] = useState('0');
  const [weeklyCents, setWeeklyCents] = useState('0');
  const [fallbackModel, setFallbackModel] = useState(DEFAULT_FALLBACK_MODEL);
  const [overrides, setOverrides] = useState<SpendLimitEntry[]>([]);
  const [overrideModalOpen, setOverrideModalOpen] = useState(false);
  const [editingOverride, setEditingOverride] = useState<SpendLimitEntry | null>(null);
  const [hashToDisplay, setHashToDisplay] = useState<Record<string, string>>({});
  const [bindings, setBindings] = useState<Map<string, EnterpriseKeyBinding>>(new Map());
  const [downgradedEntries, setDowngradedEntries] = useState<DowngradeEntry[]>([]);
  const [actionLoading, setActionLoading] = useState(false);

  const loadData = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const [quotaConfig, bindingResponse, downgradeResponse] = await Promise.all([
        quotaLimitsApi.getDowngradeConfig(),
        enterpriseKeysApi.listKeyBindings(),
        quotaPauseApi.listDowngraded(),
      ]);
      setEnabled(quotaConfig.enabled);
      setDailyCents(String(quotaConfig.default.daily_cents));
      setWeeklyCents(String(quotaConfig.default.weekly_cents));
      setFallbackModel(quotaConfig.fallback_model || DEFAULT_FALLBACK_MODEL);
      setOverrides((quotaConfig.overrides ?? []).filter((entry) => entry.apply_to === 'api-key'));

      const nextDisplay: Record<string, string> = {};
      const nextBindings = new Map<string, EnterpriseKeyBinding>();
      for (const binding of bindingResponse.items ?? []) {
        if (binding.apiKey) {
          const shortHash = quotaKeyHash(binding.apiKey).toLowerCase();
          nextDisplay[shortHash] = buildBindingLabel(binding.userName, binding.email);
          nextBindings.set(shortHash, binding);
        }
      }
      setHashToDisplay(nextDisplay);
      setBindings(nextBindings);
      setDowngradedEntries(downgradeResponse.entries ?? []);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : t('quota_downgrade.load_failed'));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    void loadData();
  }, [loadData]);

  const sortedDowngradedEntries = useMemo(
    () =>
      [...downgradedEntries].sort(
        (left, right) => new Date(right.downgraded_at).getTime() - new Date(left.downgraded_at).getTime()
      ),
    [downgradedEntries]
  );

  const keyOptions = useMemo(
    () => Object.entries(hashToDisplay).map(([hash, label]) => ({ hash, label })),
    [hashToDisplay]
  );

  const resolveUser = useCallback(
    (keyHash: string): string => {
      const normalized = keyHash.toLowerCase();
      const exact = bindings.get(normalized);
      if (exact) return buildBindingLabel(exact.userName, exact.email);
      const prefixMatch = Object.entries(hashToDisplay).find(([hash]) => hash.startsWith(normalized));
      return prefixMatch ? prefixMatch[1] : keyHash;
    },
    [bindings, hashToDisplay]
  );

  const handleSave = async () => {
    const model = fallbackModel.trim();
    if (enabled && !model) {
      showNotification(t('quota_downgrade.fallback_model_required'), 'error' as NotificationType);
      return;
    }

    setSaving(true);
    try {
      await quotaLimitsApi.updateDowngradeConfig({
        enabled,
        default: {
          daily_cents: parseInt(dailyCents, 10) || 0,
          weekly_cents: parseInt(weeklyCents, 10) || 0,
        },
        overrides,
        fallback_model: model || DEFAULT_FALLBACK_MODEL,
      });
      showNotification(t('quota_downgrade.save_success'), 'success' as NotificationType);
      void loadData();
    } catch (err: unknown) {
      showNotification(err instanceof Error ? err.message : t('quota_downgrade.save_failed'), 'error' as NotificationType);
    } finally {
      setSaving(false);
    }
  };

  const openNewOverride = () => {
    setEditingOverride({ apply_to: 'api-key', apply_value: '', daily_cents: 0, weekly_cents: 0 });
    setOverrideModalOpen(true);
  };

  const saveOverride = () => {
    if (!editingOverride?.apply_value) return;
    const entry = { ...editingOverride, apply_to: 'api-key' };
    setOverrides((current) => {
      const index = current.findIndex((item) => item.apply_value === entry.apply_value);
      if (index < 0) return [...current, entry];
      const next = [...current];
      next[index] = entry;
      return next;
    });
    setOverrideModalOpen(false);
    setEditingOverride(null);
  };

  const handleResume = async (keyHash: string) => {
    setActionLoading(true);
    try {
      await quotaPauseApi.resumeDowngradeKey(keyHash);
      setDowngradedEntries((current) => current.filter((entry) => entry.key_hash !== keyHash));
      showNotification(t('quota_downgrade.restore_success'), 'success' as NotificationType);
    } catch (err: unknown) {
      showNotification(err instanceof Error ? err.message : t('quota_downgrade.restore_failed'), 'error' as NotificationType);
    } finally {
      setActionLoading(false);
    }
  };

  if (loading) return <div className={styles.loading}>{t('common.loading')}</div>;
  if (error) return <div className={styles.error}>{error}</div>;

  return (
    <div className={styles.container}>
      <div className={styles.header}>
        <h1>{t('quota_downgrade.title')}</h1>
        <Button onClick={() => void handleSave()} disabled={saving}>
          {saving ? t('common.saving') : t('common.save')}
        </Button>
      </div>

      <Card className={styles.section}>
        <h2>{t('quota_downgrade.policy_title')}</h2>
        <div className={styles.fieldRow}>
          <label className={styles.toggle}>
            <input type="checkbox" checked={enabled} onChange={(event) => setEnabled(event.target.checked)} />
            <span>{t('quota_limits.enabled')}</span>
          </label>
        </div>
        <div className={styles.fieldRow}>
          <label>{t('quota_limits.daily_cents')}</label>
          <Input type="number" value={dailyCents} onChange={(event) => setDailyCents(event.target.value)} />
        </div>
        <div className={styles.fieldRow}>
          <label>{t('quota_limits.weekly_cents')}</label>
          <Input type="number" value={weeklyCents} onChange={(event) => setWeeklyCents(event.target.value)} />
        </div>
        <div className={styles.fieldRow}>
          <label>{t('quota_downgrade.fallback_model')}</label>
          <Input
            value={fallbackModel}
            onChange={(event) => setFallbackModel(event.target.value)}
            placeholder={t('quota_downgrade.fallback_model_placeholder')}
          />
        </div>
      </Card>

      <Card className={styles.section}>
        <div className={styles.sectionHeader}>
          <h2>{t('quota_limits.overrides')}</h2>
          <Button size="sm" onClick={openNewOverride}>{t('quota_limits.add_override')}</Button>
        </div>
        {overrides.length === 0 ? (
          <div className={styles.empty}>{t('quota_limits.no_overrides')}</div>
        ) : (
          <table className={styles.table}>
            <thead>
              <tr>
                <th>{t('quota_limits.apply_api_key')}</th>
                <th>{t('quota_limits.apply_value')}</th>
                <th>{t('quota_limits.daily_cents')}</th>
                <th>{t('quota_limits.weekly_cents')}</th>
                <th>{t('common.actions')}</th>
              </tr>
            </thead>
            <tbody>
              {overrides.map((entry) => (
                <tr key={entry.apply_value}>
                  <td>{t('quota_limits.apply_api_key')}</td>
                  <td className={styles.valueCell}>
                    {hashToDisplay[entry.apply_value] || entry.apply_value}
                    <span className={styles.valueHash}>{entry.apply_value}</span>
                  </td>
                  <td>{entry.daily_cents}</td>
                  <td>{entry.weekly_cents}</td>
                  <td>
                    <Button size="sm" variant="secondary" onClick={() => setOverrides((current) => current.filter((item) => item !== entry))}>
                      {t('common.delete')}
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Card>

      <Card className={styles.section}>
        <div className={styles.sectionHeader}>
          <h2>{t('quota_downgrade.active_title')}</h2>
          <Button size="sm" variant="secondary" onClick={() => void loadData()} disabled={actionLoading}>
            {t('common.refresh')}
          </Button>
        </div>
        {sortedDowngradedEntries.length === 0 ? (
          <div className={styles.empty}>{t('quota_downgrade.no_active')}</div>
        ) : (
          <table className={styles.table}>
            <thead>
              <tr>
                <th>{t('quota_downgrade.user')}</th>
                <th>{t('quota_downgrade.model')}</th>
                <th>{t('quota_downgrade.reason')}</th>
                <th>{t('quota_downgrade.downgraded_at')}</th>
                <th>{t('quota_downgrade.restore_at')}</th>
                <th>{t('common.actions')}</th>
              </tr>
            </thead>
            <tbody>
              {sortedDowngradedEntries.map((entry) => (
                <tr key={entry.key_hash}>
                  <td>
                    <div>{resolveUser(entry.key_hash)}</div>
                    <span className={styles.valueHash}>{entry.key_hash}</span>
                  </td>
                  <td>{entry.fallback_model}</td>
                  <td>{entry.reason || t('quota_downgrade.spend_limit_exceeded')}</td>
                  <td>{formatTime(entry.downgraded_at)}</td>
                  <td>{entry.expires_at ? formatTime(entry.expires_at) : t('quota_downgrade.permanent')}</td>
                  <td>
                    <Button size="sm" variant="secondary" disabled={actionLoading} onClick={() => void handleResume(entry.key_hash)}>
                      {t('quota_downgrade.restore_original_model')}
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Card>

      <Modal open={overrideModalOpen} onClose={() => setOverrideModalOpen(false)} title={t('quota_limits.edit_override')}>
        {editingOverride && (
          <div className={styles.form}>
            <label>{t('quota_limits.apply_api_key')}</label>
            {keyOptions.length > 0 ? (
              <Select
                value={editingOverride.apply_value}
                onChange={(value) => setEditingOverride({ ...editingOverride, apply_value: value })}
                options={[
                  { value: '', label: t('quota_limits.select_user_placeholder') },
                  ...keyOptions.map((option) => ({ value: option.hash, label: option.label })),
                ]}
              />
            ) : (
              <div className={styles.empty}>{t('quota_limits.no_key_bindings')}</div>
            )}
            <label>{t('quota_limits.daily_cents')}</label>
            <Input
              type="number"
              value={String(editingOverride.daily_cents)}
              onChange={(event) => setEditingOverride({ ...editingOverride, daily_cents: parseInt(event.target.value, 10) || 0 })}
            />
            <label>{t('quota_limits.weekly_cents')}</label>
            <Input
              type="number"
              value={String(editingOverride.weekly_cents)}
              onChange={(event) => setEditingOverride({ ...editingOverride, weekly_cents: parseInt(event.target.value, 10) || 0 })}
            />
            <div className={styles.formActions}>
              <Button variant="secondary" onClick={() => setOverrideModalOpen(false)}>{t('common.cancel')}</Button>
              <Button onClick={saveOverride} disabled={!editingOverride.apply_value}>{t('common.save')}</Button>
            </div>
          </div>
        )}
      </Modal>
    </div>
  );
}

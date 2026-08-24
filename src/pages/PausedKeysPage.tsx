import { useMemo, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { Select } from '@/components/ui/Select';
import { useNotificationStore } from '@/stores';
import type { NotificationType } from '@/types';
import { quotaPauseApi, type PauseEntry } from '@/services/api/quotaPause';
import { enterpriseKeysApi } from '@/services/api/enterpriseKeys';
import type { EnterpriseKeyBinding } from '@/types/enterpriseKey';
import { quotaKeyHash } from '@/utils/apiKeyHash';
import styles from './PausedKeysPage.module.scss';

const buildBindingLabel = (binding: EnterpriseKeyBinding): string =>
  binding.email ? `${binding.userName} (${binding.email})` : binding.userName;

export function PausedKeysPage() {
  const { t } = useTranslation();
  const { showNotification } = useNotificationStore();

  const [entries, setEntries] = useState<PauseEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [keyBindings, setKeyBindings] = useState<Map<string, EnterpriseKeyBinding>>(new Map());

  const [pauseModalOpen, setPauseModalOpen] = useState(false);
  const [pauseKeyHash, setPauseKeyHash] = useState('');
  const [pauseReason, setPauseReason] = useState('');
  const [pauseDurationSec, setPauseDurationSec] = useState('3600');

  const keyOptions = useMemo(
    () =>
      Array.from(keyBindings.values())
        .filter((binding) => binding.apiKey)
        .map((binding) => ({
          value: binding.apiKey,
          label: buildBindingLabel(binding),
        })),
    [keyBindings]
  );

  const resolveUserDisplay = (keyHash: string): string => {
    const binding = keyBindings.get(keyHash.toLowerCase());
    if (binding?.userName) return buildBindingLabel(binding);
    return t('quota_pause.unresolved_user');
  };

  const fetchEntries = async () => {
    setLoading(true);
    setError('');
    try {
      const [pauseData, bindingsData] = await Promise.all([
        quotaPauseApi.listPaused(),
        enterpriseKeysApi.listKeyBindings().catch(() => null),
      ]);
      setEntries(pauseData.entries ?? []);

      const bindingMap = new Map<string, EnterpriseKeyBinding>();
      for (const binding of bindingsData?.items ?? []) {
        if (binding.apiKey) bindingMap.set(quotaKeyHash(binding.apiKey).toLowerCase(), binding);
      }
      setKeyBindings(bindingMap);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Failed to load');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void fetchEntries();
  }, []);

  const handlePause = async () => {
    if (!pauseKeyHash.trim()) {
      showNotification(t('quota_pause.key_hash_required'), 'error' as NotificationType);
      return;
    }
    try {
      const secs = parseInt(pauseDurationSec, 10) || 0;
      await quotaPauseApi.pauseKey(pauseKeyHash.trim(), pauseReason.trim(), secs > 0 ? secs : undefined);
      showNotification(t('quota_pause.pause_success'), 'success' as NotificationType);
      setPauseModalOpen(false);
      setPauseKeyHash('');
      setPauseReason('');
      setPauseDurationSec('3600');
      void fetchEntries();
    } catch (err: unknown) {
      showNotification(err instanceof Error ? err.message : 'Failed', 'error' as NotificationType);
    }
  };

  const handleResume = async (keyHash: string) => {
    try {
      await quotaPauseApi.resumeKey(keyHash);
      showNotification(t('quota_pause.resume_success'), 'success' as NotificationType);
      void fetchEntries();
    } catch (err: unknown) {
      showNotification(err instanceof Error ? err.message : 'Failed', 'error' as NotificationType);
    }
  };

  const formatTime = (iso: string) => {
    if (!iso) return '-';
    return new Date(iso).toLocaleString();
  };

  return (
    <div className={styles.container}>
      <div className={styles.header}>
        <h1>{t('quota_pause.title')}</h1>
        <Button onClick={() => setPauseModalOpen(true)}>{t('quota_pause.manual_pause')}</Button>
      </div>

      {error && <div className={styles.error}>{error}</div>}

      <Card className={styles.tableCard}>
        {loading ? (
          <div className={styles.loading}>{t('common.loading')}</div>
        ) : entries.length === 0 ? (
          <div className={styles.empty}>{t('quota_pause.no_paused_keys')}</div>
        ) : (
          <table className={styles.table}>
            <thead>
              <tr>
                <th>{t('quota_pause.user_name')}</th>
                <th>{t('quota_pause.key_hash')}</th>
                <th>{t('quota_pause.reason')}</th>
                <th>{t('quota_pause.paused_at')}</th>
                <th>{t('quota_pause.expires_at')}</th>
                <th>{t('common.actions')}</th>
              </tr>
            </thead>
            <tbody>
              {entries.map((entry) => (
                <tr key={entry.key_hash}>
                  <td className={styles.userCell}>{resolveUserDisplay(entry.key_hash)}</td>
                  <td className={styles.hashCell}>{entry.key_hash}</td>
                  <td>{entry.reason || '-'}</td>
                  <td>{formatTime(entry.paused_at)}</td>
                  <td>{entry.expires_at ? formatTime(entry.expires_at) : t('quota_pause.permanent')}</td>
                  <td>
                    <Button size="sm" variant="secondary" onClick={() => handleResume(entry.key_hash)}>
                      {t('quota_pause.resume')}
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Card>

      <Modal open={pauseModalOpen} onClose={() => setPauseModalOpen(false)} title={t('quota_pause.manual_pause')}>
        <div className={styles.form}>
          <label>{t('quota_pause.user_name')}</label>
          {keyOptions.length > 0 ? (
            <Select
              value={pauseKeyHash}
              onChange={setPauseKeyHash}
              options={[{ value: '', label: t('quota_limits.select_user_placeholder') }, ...keyOptions]}
            />
          ) : (
            <div className={styles.empty}>{t('quota_limits.no_key_bindings')}</div>
          )}

          <label>{t('quota_pause.reason')}</label>
          <Input value={pauseReason} onChange={(e) => setPauseReason(e.target.value)} placeholder={t('quota_pause.reason_placeholder')} />

          <label>{t('quota_pause.duration_seconds')}</label>
          <Input type="number" value={pauseDurationSec} onChange={(e) => setPauseDurationSec(e.target.value)} placeholder="3600" />

          <div className={styles.formActions}>
            <Button variant="secondary" onClick={() => setPauseModalOpen(false)}>{t('common.cancel')}</Button>
            <Button onClick={handlePause} disabled={!pauseKeyHash}>{t('quota_pause.confirm_pause')}</Button>
          </div>
        </div>
      </Modal>
    </div>
  );
}
